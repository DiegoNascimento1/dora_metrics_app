import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { catchError, finalize, forkJoin, of } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import { Project, Team } from '../../core/api/api.types';
import { SkeletonComponent } from '../../shared/skeleton.component';
import { EmptyStateComponent } from '../../shared/empty-state.component';

@Component({
  selector: 'app-projects',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    RouterLink,
    MatCardModule,
    MatTableModule,
    MatButtonModule,
    MatIconModule,
    SkeletonComponent,
    EmptyStateComponent,
  ],
  template: `
    <h1>Projetos</h1>

    @if (loading()) {
      <mat-card appearance="outlined" class="skel-card">
        @for (_ of [0, 1, 2]; track $index) {
          <div class="skel-row">
            <app-skeleton variant="text" width="240px" />
            <app-skeleton variant="text" width="320px" />
            <app-skeleton variant="chip" width="60px" />
          </div>
        }
      </mat-card>
    } @else if (projects().length === 0) {
      <mat-card appearance="outlined">
        <mat-card-content>
          <app-empty-state
            icon="folder_open"
            title="Nenhum projeto cadastrado"
            description="Conecte uma instância GitLab primeiro e depois adicione um projeto via CLI ou na próxima fatia (admin endpoint)."
          >
            <a mat-flat-button color="primary" routerLink="/settings">
              <mat-icon>cable</mat-icon>
              Ir para integrações
            </a>
          </app-empty-state>
        </mat-card-content>
      </mat-card>
    } @else {
      <mat-card appearance="outlined">
        <table mat-table [dataSource]="projects()">
          <ng-container matColumnDef="path">
            <th mat-header-cell *matHeaderCellDef>Path</th>
            <td mat-cell *matCellDef="let p">{{ p.pathWithNamespace }}</td>
          </ng-container>
          <ng-container matColumnDef="team">
            <th mat-header-cell *matHeaderCellDef>Time</th>
            <td mat-cell *matCellDef="let p">
              @let team = teamFor(p.teamId);
              @if (team) {
                <span class="team-chip" [style.background]="team.color || '#475569'">
                  <span class="team-chip-emoji">{{ team.emoji || '👥' }}</span>
                  {{ team.name }}
                </span>
              } @else {
                <span class="muted">—</span>
              }
            </td>
          </ng-container>
          <ng-container matColumnDef="id">
            <th mat-header-cell *matHeaderCellDef>ID</th>
            <td mat-cell *matCellDef="let p"><code>{{ p.id }}</code></td>
          </ng-container>
          <ng-container matColumnDef="active">
            <th mat-header-cell *matHeaderCellDef>Status</th>
            <td mat-cell *matCellDef="let p">
              <span class="status-chip" [class.status-active]="p.active">
                {{ p.active ? 'ativo' : 'inativo' }}
              </span>
            </td>
          </ng-container>

          <tr mat-header-row *matHeaderRowDef="cols"></tr>
          <tr mat-row *matRowDef="let row; columns: cols"></tr>
        </table>
      </mat-card>
    }
  `,
  styles: [
    `
      table {
        width: 100%;
      }
      code {
        font-size: 0.875rem;
        color: var(--color-text-secondary);
      }
      .skel-card {
        padding: var(--space-4);
      }
      .skel-row {
        display: flex;
        gap: var(--space-4);
        padding: var(--space-3) 0;
        align-items: center;
        border-bottom: 1px solid var(--color-border);
      }
      .skel-row:last-child {
        border-bottom: none;
      }
      .status-chip {
        display: inline-block;
        padding: 2px 10px;
        border-radius: 999px;
        font-size: var(--font-size-xs);
        font-weight: 600;
        background: var(--color-tier-na-bg);
        color: var(--color-tier-na);
      }
      .status-chip.status-active {
        background: var(--color-tier-elite-bg);
        color: var(--color-tier-elite);
      }
      .team-chip {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        padding: 3px 10px;
        border-radius: 999px;
        color: white;
        font-size: var(--font-size-xs);
        font-weight: 600;
      }
      .team-chip-emoji {
        font-size: 14px;
        line-height: 1;
      }
    `,
  ],
})
export class ProjectsComponent {
  private api = inject(ApiClient);

  loading = signal(false);
  projects = signal<Project[]>([]);
  teams = signal<Team[]>([]);
  cols = ['path', 'team', 'id', 'active'];

  private teamsById = computed(() => {
    const m = new Map<string, Team>();
    for (const t of this.teams()) m.set(t.id, t);
    return m;
  });

  teamFor(teamId: string | null | undefined): Team | undefined {
    if (!teamId) return undefined;
    return this.teamsById().get(teamId);
  }

  constructor() {
    this.loading.set(true);
    forkJoin({
      projects: this.api.listProjects().pipe(catchError(() => of([] as Project[]))),
      teams: this.api
        .listTeams('acme')
        .pipe(catchError(() => of([] as Team[]))),
    })
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe(({ projects, teams }) => {
        this.projects.set(projects);
        this.teams.set(teams);
      });
  }
}
