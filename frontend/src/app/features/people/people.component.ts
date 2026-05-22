import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import {
  CdkDrag,
  CdkDragDrop,
  CdkDropList,
  CdkDropListGroup,
} from '@angular/cdk/drag-drop';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { forkJoin, of, catchError, finalize, Observable } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import { SkeletonComponent } from '../../shared/skeleton.component';
import { EmptyStateComponent } from '../../shared/empty-state.component';
import {
  Identity,
  MergeSuggestion,
  PersonMetrics,
  PersonWithIdentities,
} from '../../core/api/api.types';

interface MetricTile {
  label: string;
  value: string;
}

type Window = '7d' | '30d' | '90d';

@Component({
  selector: 'app-people',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    CdkDrag,
    CdkDropList,
    CdkDropListGroup,
    MatCardModule,
    MatTableModule,
    MatButtonModule,
    MatIconModule,
    MatChipsModule,
    MatFormFieldModule,
    MatInputModule,
    MatSnackBarModule,
    SkeletonComponent,
    EmptyStateComponent,
  ],
  template: `
    <h1>Pessoas e identidades</h1>

    <div class="filters">
      <mat-form-field appearance="outline">
        <mat-label>Tenant</mat-label>
        <input matInput [(ngModel)]="tenant" (change)="reload()" placeholder="acme" />
      </mat-form-field>
      <button mat-stroked-button (click)="reload()">
        <mat-icon fontIcon="refresh"></mat-icon> Atualizar
      </button>
    </div>

    @if (loading()) {
      <mat-card appearance="outlined" class="skel-card">
        <app-skeleton variant="title" width="30%" />
        @for (_ of [0, 1, 2]; track $index) {
          <div class="skel-row">
            <app-skeleton variant="chip" width="60px" />
            <app-skeleton variant="text" width="200px" />
            <app-skeleton variant="text" width="240px" />
          </div>
        }
      </mat-card>
    } @else {
      <!-- Sugestões automáticas -->
      @if (suggestions().length > 0) {
        <mat-card appearance="outlined" class="block">
          <mat-card-header>
            <mat-card-title>
              Sugestões de merge ({{ suggestions().length }})
            </mat-card-title>
            <mat-card-subtitle>
              O coach (você) decide. Cor da pontuação é puramente visual.
            </mat-card-subtitle>
          </mat-card-header>
          <mat-card-content>
            <table mat-table [dataSource]="suggestions()">
              <ng-container matColumnDef="gitlab">
                <th mat-header-cell *matHeaderCellDef>GitLab</th>
                <td mat-cell *matCellDef="let s">
                  <span class="src-chip src-gitlab">
                    {{ pickGitLab(s).externalUsername }}
                  </span>
                  <span class="muted">{{ pickGitLab(s).externalEmail ?? '' }}</span>
                </td>
              </ng-container>
              <ng-container matColumnDef="jira">
                <th mat-header-cell *matHeaderCellDef>Jira</th>
                <td mat-cell *matCellDef="let s">
                  <span class="src-chip src-jira">
                    {{ pickJira(s).externalUsername }}
                  </span>
                  <span class="muted">{{ pickJira(s).externalEmail ?? '' }}</span>
                </td>
              </ng-container>
              <ng-container matColumnDef="reason">
                <th mat-header-cell *matHeaderCellDef>Confiança</th>
                <td mat-cell *matCellDef="let s">
                  <span class="confidence" [class.confidence-high]="s.score >= 1">
                    {{ (s.score * 100).toFixed(0) }}% · {{ s.reason }}
                  </span>
                </td>
              </ng-container>
              <ng-container matColumnDef="action">
                <th mat-header-cell *matHeaderCellDef></th>
                <td mat-cell *matCellDef="let s">
                  <button mat-flat-button color="primary" (click)="applySuggestion(s)">
                    Aceitar merge
                  </button>
                </td>
              </ng-container>
              <tr mat-header-row *matHeaderRowDef="suggestionCols"></tr>
              <tr mat-row *matRowDef="let row; columns: suggestionCols"></tr>
            </table>
          </mat-card-content>
        </mat-card>
      }

      <p class="hint">
        <mat-icon class="hint-icon" fontIcon="drag_indicator"></mat-icon>
        Arraste uma identidade da lista para o card de uma pessoa para vincular.
      </p>

      <div cdkDropListGroup class="dnd-container">
        <!-- Pessoas (drop targets) -->
        <div class="people-col">
          <h2>Pessoas ({{ people().length }})</h2>

          @if (people().length === 0) {
            <mat-card appearance="outlined">
              <mat-card-content>
                <app-empty-state
                  icon="group"
                  title="Nenhuma pessoa ainda"
                  description="Aceite uma sugestão automática ou arraste uma identidade da coluna ao lado para criar a primeira pessoa."
                />
              </mat-card-content>
            </mat-card>
          }

          @for (p of people(); track p.id) {
            <mat-card
              appearance="outlined"
              class="person-card"
              cdkDropList
              [cdkDropListData]="p"
              (cdkDropListDropped)="onDropOnPerson($event, p)"
            >
              <mat-card-header>
                <mat-card-title>{{ p.displayName }}</mat-card-title>
                @if (p.primaryEmail) {
                  <mat-card-subtitle>{{ p.primaryEmail }}</mat-card-subtitle>
                }
              </mat-card-header>
              <mat-card-content>
                <div class="identities">
                  @for (id of p.identities; track id.id) {
                    <span [class]="'src-chip src-' + id.kind">
                      {{ id.kind }}: {{ id.externalUsername }}
                    </span>
                  }
                  @if (p.identities.length === 0) {
                    <span class="muted">Solte uma identidade aqui</span>
                  }
                </div>
                @if (metricsByPerson()[p.id]; as m) {
                  <div class="person-metrics">
                    <span class="metric">
                      <b>{{ m.deploymentsTriggered }}</b>
                      <span class="muted">deploys</span>
                    </span>
                    <span class="metric">
                      <b>{{ formatLT(m.leadTimeMedianSeconds) }}</b>
                      <span class="muted">LT mediano ({{ m.leadTimeSampleSize }})</span>
                    </span>
                    <span class="metric">
                      <b>{{ m.incidentsLinked }}</b>
                      <span class="muted">incidents</span>
                    </span>
                    <span class="muted">· 30d</span>
                  </div>
                }
              </mat-card-content>
            </mat-card>
          }
        </div>

        <!-- Identidades não-vinculadas (drag source) -->
        <div class="unlinked-col">
          <h2>Não vinculadas ({{ unlinked().length }})</h2>

          @if (unlinked().length === 0) {
            <mat-card appearance="outlined">
              <mat-card-content>
                <app-empty-state
                  icon="task_alt"
                  title="Tudo vinculado"
                  description="Não há identidades pendentes de merge. Próximos usernames vistos em commits/deploys vão aparecer aqui."
                />
              </mat-card-content>
            </mat-card>
          } @else {
            <div
              cdkDropList
              [cdkDropListData]="unlinked()"
              [cdkDropListSortingDisabled]="true"
              class="unlinked-list"
            >
              @for (i of unlinked(); track i.id) {
                <div class="identity-card" cdkDrag [cdkDragData]="i">
                  <div class="drag-handle">
                    <mat-icon fontIcon="drag_indicator"></mat-icon>
                  </div>
                  <div>
                    <span [class]="'src-chip src-' + i.kind">{{ i.kind }}</span>
                    <strong class="username">{{ i.externalUsername }}</strong>
                    @if (i.externalEmail) {
                      <div class="muted">{{ i.externalEmail }}</div>
                    }
                  </div>
                  <button
                    mat-icon-button
                    (click)="createFromIdentity(i)"
                    matTooltip="Criar nova pessoa a partir desta identidade"
                    aria-label="Criar nova pessoa"
                  >
                    <mat-icon fontIcon="person_add"></mat-icon>
                  </button>
                </div>
              }
            </div>
          }
        </div>
      </div>
    }
  `,
  styles: [
    `
      .filters {
        display: flex;
        align-items: center;
        gap: var(--space-4);
        margin: var(--space-4) 0;
      }
      .block {
        margin-top: var(--space-5);
      }
      .hint {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        color: var(--color-text-secondary);
        margin: var(--space-5) 0 var(--space-3);
        font-size: var(--font-size-sm);
      }
      .hint-icon {
        font-size: 20px;
        height: 20px;
        width: 20px;
        color: var(--color-text-muted);
      }

      .dnd-container {
        display: grid;
        grid-template-columns: 2fr 1fr;
        gap: var(--space-5);
      }
      @media (max-width: 960px) {
        .dnd-container {
          grid-template-columns: 1fr;
        }
      }

      h2 {
        margin: 0 0 var(--space-3);
      }

      .person-card {
        margin-bottom: var(--space-4);
        transition: background var(--transition-fast), border-color var(--transition-fast);
      }
      .person-card.cdk-drop-list-receiving,
      .person-card.cdk-drop-list-dragging {
        border-color: var(--color-brand) !important;
        background: var(--color-tier-high-bg) !important;
      }

      .unlinked-list {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
      }
      .identity-card {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        padding: var(--space-3);
        background: var(--color-bg-elevated);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-md);
        cursor: grab;
        transition: box-shadow var(--transition-fast), border-color var(--transition-fast);
      }
      .identity-card:hover {
        border-color: var(--color-brand);
        box-shadow: var(--shadow-md);
      }
      .identity-card:active {
        cursor: grabbing;
      }
      .drag-handle {
        color: var(--color-text-muted);
        display: flex;
        align-items: center;
      }
      .username {
        display: block;
        margin-top: var(--space-1);
        font-family: var(--font-mono);
        font-size: var(--font-size-sm);
      }

      .identities {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
        min-height: 32px;
        padding: var(--space-2);
        border-radius: var(--radius-sm);
        background: var(--color-surface);
      }

      .person-metrics {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-4);
        margin-top: var(--space-3);
        padding-top: var(--space-3);
        border-top: 1px dashed var(--color-border);
        font-size: var(--font-size-sm);
      }
      .person-metrics b {
        color: var(--color-brand);
        font-size: var(--font-size-base);
        font-weight: 600;
      }
      .metric {
        display: inline-flex;
        align-items: baseline;
        gap: var(--space-1);
      }

      .src-chip {
        display: inline-flex;
        align-items: center;
        padding: var(--space-1) var(--space-2);
        border-radius: var(--radius-sm);
        font-size: var(--font-size-xs);
        font-weight: 600;
        color: white;
        text-transform: lowercase;
      }
      .src-gitlab { background: var(--color-gitlab); }
      .src-jira   { background: var(--color-jira); }

      .confidence {
        font-family: var(--font-mono);
        font-size: var(--font-size-sm);
        color: var(--color-text-secondary);
      }
      .confidence-high {
        color: var(--color-tier-elite);
        font-weight: 600;
      }
      .skel-card {
        padding: var(--space-4);
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .skel-row {
        display: flex;
        gap: var(--space-3);
        padding: var(--space-2) 0;
        align-items: center;
      }
    `,
  ],
})
export class PeopleComponent {
  private api = inject(ApiClient);
  private snack = inject(MatSnackBar);

  tenant = 'acme';
  loading = signal(false);
  people = signal<PersonWithIdentities[]>([]);
  unlinked = signal<Identity[]>([]);
  suggestions = signal<MergeSuggestion[]>([]);
  metricsByPerson = signal<Record<string, PersonMetrics>>({});

  suggestionCols = ['gitlab', 'jira', 'reason', 'action'];

  constructor() {
    this.reload();
  }

  reload(): void {
    if (!this.tenant) return;
    this.loading.set(true);
    forkJoin({
      people: this.api.listPeople(this.tenant).pipe(catchError(() => of([]))),
      unlinked: this.api
        .listUnlinkedIdentities(this.tenant)
        .pipe(catchError(() => of([]))),
      suggestions: this.api
        .getAutomatchSuggestions(this.tenant)
        .pipe(catchError(() => of([]))),
    })
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe(({ people, unlinked, suggestions }) => {
        this.people.set(people);
        this.unlinked.set(unlinked);
        this.suggestions.set(suggestions);
        this.loadMetrics(people);
      });
  }

  private loadMetrics(people: PersonWithIdentities[]): void {
    if (people.length === 0) {
      this.metricsByPerson.set({});
      return;
    }
    const requests: Record<string, Observable<PersonMetrics | null>> = {};
    for (const p of people) {
      requests[p.id] = this.api
        .getPersonMetrics(p.id, '30d')
        .pipe(catchError(() => of<PersonMetrics | null>(null)));
    }
    forkJoin(requests).subscribe((res) => {
      const next: Record<string, PersonMetrics> = {};
      for (const [id, m] of Object.entries(res)) {
        if (m) next[id] = m;
      }
      this.metricsByPerson.set(next);
    });
  }

  pickGitLab(s: MergeSuggestion): Identity {
    return s.a.kind === 'gitlab' ? s.a : s.b;
  }

  pickJira(s: MergeSuggestion): Identity {
    return s.a.kind === 'jira' ? s.a : s.b;
  }

  applySuggestion(s: MergeSuggestion): void {
    const gl = this.pickGitLab(s);
    const jr = this.pickJira(s);
    const displayName = gl.externalEmail ?? jr.externalEmail ?? gl.externalUsername;
    const email = gl.externalEmail ?? jr.externalEmail ?? '';

    this.api
      .createPerson({
        tenant: this.tenant,
        displayName,
        primaryEmail: email,
        identityIds: [gl.id, jr.id],
      })
      .subscribe({
        next: () => {
          this.snack.open(
            `Pessoa criada e ${gl.kind}/${jr.kind} vinculados`,
            'OK',
            { duration: 3000 },
          );
          this.reload();
        },
        error: (err) => this.handleError(err),
      });
  }

  createFromIdentity(i: Identity): void {
    this.api
      .createPerson({
        tenant: this.tenant,
        displayName: i.externalEmail ?? i.externalUsername,
        primaryEmail: i.externalEmail ?? '',
        identityIds: [i.id],
      })
      .subscribe({
        next: () => {
          this.snack.open('Pessoa criada e identidade vinculada', 'OK', {
            duration: 3000,
          });
          this.reload();
        },
        error: (err) => this.handleError(err),
      });
  }

  onDropOnPerson(
    event: CdkDragDrop<PersonWithIdentities, Identity[], Identity>,
    person: PersonWithIdentities,
  ): void {
    // Tipos garantem que veio da coluna unlinked (Identity[]).
    const identity = event.item.data;
    if (!identity) return;

    this.api
      .linkIdentity(identity.id, { personId: person.id, linkedBy: 'ui' })
      .subscribe({
        next: () => {
          this.snack.open(
            `${identity.kind}:${identity.externalUsername} → ${person.displayName}`,
            'OK',
            { duration: 3000 },
          );
          this.reload();
        },
        error: (err) => this.handleError(err),
      });
  }

  formatLT(seconds: number | null): string {
    if (seconds === null || seconds === undefined) return '—';
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${(seconds / 60).toFixed(0)}min`;
    if (seconds < 86400) return `${(seconds / 3600).toFixed(1)}h`;
    return `${(seconds / 86400).toFixed(1)}d`;
  }

  private handleError(err: unknown): void {
    const e = err as { error?: string; message?: string };
    this.snack.open(`Erro: ${e?.error ?? e?.message ?? err}`, 'OK', {
      duration: 5000,
    });
  }
}
