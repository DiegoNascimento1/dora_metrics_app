import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatCardModule } from '@angular/material/card';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatMenuModule } from '@angular/material/menu';
import { MatSelectModule } from '@angular/material/select';
import { MatDialog, MatDialogModule } from '@angular/material/dialog';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { catchError, finalize, forkJoin, of } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import { Project, Team } from '../../core/api/api.types';
import { SkeletonComponent } from '../../shared/skeleton.component';
import { EmptyStateComponent } from '../../shared/empty-state.component';
import {
  TeamDialogComponent,
  TeamDialogData,
  TeamDialogResult,
} from './team-dialog.component';

@Component({
  selector: 'app-teams',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    MatCardModule,
    MatButtonModule,
    MatIconModule,
    MatFormFieldModule,
    MatInputModule,
    MatMenuModule,
    MatSelectModule,
    MatDialogModule,
    MatSnackBarModule,
    SkeletonComponent,
    EmptyStateComponent,
  ],
  template: `
    <div class="head">
      <h1>Times</h1>
      <button mat-flat-button color="primary" (click)="openCreate()">
        <mat-icon fontIcon="add"></mat-icon>
        Novo time
      </button>
    </div>

    <div class="filters">
      <mat-form-field appearance="outline">
        <mat-label>Tenant</mat-label>
        <input matInput [(ngModel)]="tenant" (change)="reload()" placeholder="acme" />
      </mat-form-field>
    </div>

    @if (loading()) {
      <div class="grid">
        @for (_ of [0, 1, 2]; track $index) {
          <mat-card appearance="outlined" class="skel-card">
            <app-skeleton variant="circle" width="48px" height="48px" />
            <app-skeleton variant="title" width="60%" />
            <app-skeleton variant="text" width="40%" />
          </mat-card>
        }
      </div>
    } @else if (teams().length === 0) {
      <mat-card appearance="outlined">
        <mat-card-content>
          <app-empty-state
            icon="groups"
            title="Nenhum time criado ainda"
            description="Crie seu primeiro time para agrupar projetos. Cada time ganha cor + emoji para identidade visual e poderá ter métricas DORA agregadas."
          >
            <button mat-flat-button color="primary" (click)="openCreate()">
              <mat-icon fontIcon="add"></mat-icon>
              Criar primeiro time
            </button>
          </app-empty-state>
        </mat-card-content>
      </mat-card>
    } @else {
      <div class="grid">
        @for (t of teams(); track t.id) {
          <mat-card appearance="outlined" class="team-card">
            <div class="team-head" [style.background]="t.color || '#475569'">
              <span class="team-emoji">{{ t.emoji || '👥' }}</span>
              <div class="team-id">
                <div class="team-name">{{ t.name }}</div>
                <div class="team-slug"><code>{{ t.slug }}</code></div>
              </div>
              <button
                mat-icon-button
                [matMenuTriggerFor]="teamMenu"
                class="team-menu-btn"
                [attr.aria-label]="'Menu do time ' + t.name"
              >
                <mat-icon fontIcon="more_vert"></mat-icon>
              </button>
              <mat-menu #teamMenu="matMenu">
                <button mat-menu-item (click)="openEdit(t)">
                  <mat-icon fontIcon="edit"></mat-icon>
                  <span>Editar</span>
                </button>
                <button mat-menu-item (click)="removeTeam(t)">
                  <mat-icon color="warn" fontIcon="delete_outline"></mat-icon>
                  <span>Excluir</span>
                </button>
              </mat-menu>
            </div>
            <mat-card-content class="team-body">
              <div class="member-count">
                <mat-icon class="count-icon" fontIcon="folder"></mat-icon>
                <span>{{ projectsForTeam(t.id).length }}</span>
                <span class="muted">projetos</span>
              </div>

              @if (projectsForTeam(t.id).length > 0) {
                <ul class="project-list">
                  @for (p of projectsForTeam(t.id); track p.id) {
                    <li>
                      <span class="proj-path">{{ p.pathWithNamespace }}</span>
                      <button
                        mat-icon-button
                        class="unassign-btn"
                        (click)="unassignProject(p)"
                        matTooltip="Remover do time"
                        [attr.aria-label]="'Remover ' + p.pathWithNamespace + ' do time'"
                      >
                        <mat-icon fontIcon="link_off"></mat-icon>
                      </button>
                    </li>
                  }
                </ul>
              }

              @if (unassignedProjects().length > 0) {
                <mat-form-field appearance="outline" class="assign-field">
                  <mat-label>Adicionar projeto ao time</mat-label>
                  <mat-select
                    [(value)]="newAssignment[t.id]"
                    (selectionChange)="assignProject(t, $event.value)"
                  >
                    @for (p of unassignedProjects(); track p.id) {
                      <mat-option [value]="p.id">{{ p.pathWithNamespace }}</mat-option>
                    }
                  </mat-select>
                </mat-form-field>
              }
            </mat-card-content>
          </mat-card>
        }
      </div>
    }
  `,
  styles: [
    `
      .head {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: var(--space-3);
      }
      .head h1 {
        margin: 0;
      }
      .filters {
        margin-bottom: var(--space-4);
      }
      .grid {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
        gap: var(--space-4);
      }
      .skel-card {
        padding: var(--space-4);
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }

      .team-card {
        overflow: hidden;
        padding: 0 !important;
      }
      .team-head {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        padding: var(--space-3) var(--space-4);
        color: white;
        position: relative;
      }
      .team-emoji {
        font-size: 32px;
        line-height: 1;
        background: rgb(255 255 255 / 0.2);
        border-radius: var(--radius-md);
        padding: 6px 10px;
      }
      .team-id {
        flex: 1;
        min-width: 0;
      }
      .team-name {
        font-weight: 700;
        font-size: var(--font-size-lg);
        letter-spacing: -0.01em;
      }
      .team-slug code {
        opacity: 0.85;
        font-size: var(--font-size-xs);
        font-family: var(--font-mono);
      }
      .team-menu-btn {
        color: white !important;
      }

      .team-body {
        padding: var(--space-4) !important;
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .member-count {
        display: flex;
        align-items: baseline;
        gap: 6px;
        font-weight: 600;
      }
      .count-icon {
        font-size: 18px;
        height: 18px;
        width: 18px;
        color: var(--color-text-muted);
        transform: translateY(2px);
      }
      .project-list {
        list-style: none;
        padding: 0;
        margin: 0;
        display: flex;
        flex-direction: column;
        gap: 2px;
      }
      .project-list li {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 4px var(--space-2);
        border-radius: var(--radius-sm);
        transition: background var(--transition-fast);
      }
      .project-list li:hover {
        background: var(--color-surface-subtle);
      }
      .proj-path {
        font-family: var(--font-mono);
        font-size: var(--font-size-sm);
        color: var(--color-text-secondary);
      }
      .unassign-btn {
        width: 28px;
        height: 28px;
        line-height: 28px;
      }
      .unassign-btn mat-icon {
        font-size: 16px;
        height: 16px;
        width: 16px;
      }
      .assign-field {
        margin-top: var(--space-2);
      }
    `,
  ],
})
export class TeamsComponent {
  private api = inject(ApiClient);
  private dialog = inject(MatDialog);
  private snack = inject(MatSnackBar);

  tenant = 'acme';
  loading = signal(false);
  teams = signal<Team[]>([]);
  projects = signal<Project[]>([]);

  newAssignment: Record<string, string | null> = {};

  unassignedProjects = computed(() =>
    this.projects().filter((p) => !p.teamId),
  );

  constructor() {
    this.reload();
  }

  reload(): void {
    if (!this.tenant) return;
    this.loading.set(true);
    forkJoin({
      teams: this.api.listTeams(this.tenant).pipe(catchError(() => of([] as Team[]))),
      projects: this.api.listProjects().pipe(catchError(() => of([] as Project[]))),
    })
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe(({ teams, projects }) => {
        this.teams.set(teams);
        this.projects.set(projects);
      });
  }

  projectsForTeam(teamId: string): Project[] {
    return this.projects().filter((p) => p.teamId === teamId);
  }

  openCreate(): void {
    const data: TeamDialogData = { tenant: this.tenant };
    this.dialog
      .open(TeamDialogComponent, {
        data,
        width: 'min(520px, 92vw)',
        autoFocus: 'first-tabbable',
      })
      .afterClosed()
      .subscribe((result?: TeamDialogResult) => {
        if (!result || result.mode !== 'create') return;
        this.api
          .createTeam({
            tenant: this.tenant,
            slug: result.slug,
            name: result.name,
            color: result.color,
            emoji: result.emoji,
          })
          .subscribe({
            next: () => {
              this.snack.open(`Time ${result.name} criado`, 'OK', { duration: 3000 });
              this.reload();
            },
            error: (err) => this.handleError(err),
          });
      });
  }

  openEdit(team: Team): void {
    const data: TeamDialogData = { tenant: this.tenant, team };
    this.dialog
      .open(TeamDialogComponent, {
        data,
        width: 'min(520px, 92vw)',
        autoFocus: 'first-tabbable',
      })
      .afterClosed()
      .subscribe((result?: TeamDialogResult) => {
        if (!result || result.mode !== 'update') return;
        this.api
          .updateTeam(team.id, {
            name: result.name,
            color: result.color,
            emoji: result.emoji,
          })
          .subscribe({
            next: () => {
              this.snack.open('Time atualizado', 'OK', { duration: 3000 });
              this.reload();
            },
            error: (err) => this.handleError(err),
          });
      });
  }

  removeTeam(team: Team): void {
    const count = this.projectsForTeam(team.id).length;
    const msg =
      count > 0
        ? `Excluir ${team.name}? Os ${count} projeto(s) ficarão sem time (não serão removidos).`
        : `Excluir ${team.name}?`;
    if (!confirm(msg)) return;
    this.api.deleteTeam(team.id).subscribe({
      next: () => {
        this.snack.open('Time removido', 'OK', { duration: 3000 });
        this.reload();
      },
      error: (err) => this.handleError(err),
    });
  }

  assignProject(team: Team, projectId: string): void {
    if (!projectId) return;
    this.api.assignProjectToTeam(team.id, projectId).subscribe({
      next: () => {
        this.snack.open(`Projeto adicionado ao time ${team.name}`, 'OK', {
          duration: 3000,
        });
        this.newAssignment[team.id] = null;
        this.reload();
      },
      error: (err) => this.handleError(err),
    });
  }

  unassignProject(project: Project): void {
    this.api.unassignProjectFromTeam(project.id).subscribe({
      next: () => {
        this.snack.open('Projeto removido do time', 'OK', { duration: 3000 });
        this.reload();
      },
      error: (err) => this.handleError(err),
    });
  }

  private handleError(err: unknown): void {
    const e = err as { error?: string; message?: string };
    this.snack.open(`Erro: ${e?.error ?? e?.message ?? err}`, 'OK', {
      duration: 5000,
    });
  }
}
