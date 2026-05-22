import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { catchError, finalize, forkJoin, of, Observable } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import {
  Classification,
  DoraMetrics,
  Team,
} from '../../core/api/api.types';
import { SkeletonComponent } from '../../shared/skeleton.component';
import { EmptyStateComponent } from '../../shared/empty-state.component';

type Window = '7d' | '30d' | '90d';

interface LeaderRow {
  team: Team;
  metrics: DoraMetrics | null;
}

// Rank por tier (Elite=4 ... Low=1, sem dados=0).
// Anti-padrão registrado em docs/07-roadmap.md: bottom-team aparece como
// "in growth", NUNCA como "perdedor". Cores e copy refletem isso.
const TIER_RANK: Record<Classification, number> = {
  elite: 4,
  high: 3,
  medium: 2,
  low: 1,
  insufficient_data: 0,
};

@Component({
  selector: 'app-leaderboard',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    RouterLink,
    MatCardModule,
    MatChipsModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatButtonModule,
    MatIconModule,
    SkeletonComponent,
    EmptyStateComponent,
  ],
  template: `
    <div class="head">
      <div>
        <h1>Leaderboard</h1>
        <p class="muted">
          Comparação entre times do tenant. Celebração coletiva, não ranking
          punitivo — todo time tem ciclo, e o "último" é "em crescimento".
        </p>
      </div>
      <div class="head-actions">
        <mat-form-field appearance="outline">
          <mat-label>Tenant</mat-label>
          <input matInput [(ngModel)]="tenant" (change)="reload()" placeholder="acme" />
        </mat-form-field>
        <mat-form-field appearance="outline">
          <mat-label>Janela</mat-label>
          <mat-select [(value)]="selectedWindow" (selectionChange)="reload()">
            <mat-option value="7d">7 dias</mat-option>
            <mat-option value="30d">30 dias</mat-option>
            <mat-option value="90d">90 dias</mat-option>
          </mat-select>
        </mat-form-field>
      </div>
    </div>

    @if (loading()) {
      <div class="stack">
        @for (_ of [0, 1, 2]; track $index) {
          <mat-card appearance="outlined" class="leader-card skel">
            <app-skeleton variant="circle" width="48px" height="48px" />
            <app-skeleton variant="title" width="180px" />
            <app-skeleton variant="chip" width="80px" />
            <app-skeleton variant="text" width="320px" />
          </mat-card>
        }
      </div>
    } @else if (rows().length === 0) {
      <mat-card appearance="outlined">
        <mat-card-content>
          <app-empty-state
            icon="groups"
            title="Sem times pra ranquear"
            description="Crie pelo menos um time antes — atribua projetos pra ele e volte aqui."
          >
            <a mat-flat-button color="primary" routerLink="/teams">
              <mat-icon fontIcon="add"></mat-icon>
              Ir para times
            </a>
          </app-empty-state>
        </mat-card-content>
      </mat-card>
    } @else {
      <div class="stack">
        @for (r of rows(); track r.team.id; let i = $index) {
          <mat-card appearance="outlined" class="leader-card">
            <div class="rank">
              <span class="rank-num">{{ i + 1 }}</span>
              <span class="rank-suffix">{{ i === 0 ? 'º' : 'º' }}</span>
            </div>

            <div class="team" [style.background]="r.team.color || '#475569'">
              <span class="team-emoji">{{ r.team.emoji || '👥' }}</span>
              <span class="team-name">{{ r.team.name }}</span>
            </div>

            <div class="tier">
              <span [class]="'tier-' + (r.metrics?.classification ?? 'insufficient_data')">
                {{ r.metrics?.classification ?? 'insufficient_data' }}
              </span>
            </div>

            <div class="stats">
              <div class="stat">
                <div class="stat-label">DF</div>
                <div class="stat-value">
                  {{ r.metrics ? r.metrics.deploymentFrequency.toFixed(2) + '/d' : '—' }}
                </div>
              </div>
              <div class="stat">
                <div class="stat-label">LT</div>
                <div class="stat-value">
                  {{ formatLT(r.metrics?.leadTimeMedianSeconds) }}
                </div>
              </div>
              <div class="stat">
                <div class="stat-label">CFR</div>
                <div class="stat-value">
                  {{ formatCFR(r.metrics?.changeFailureRate) }}
                </div>
              </div>
              <div class="stat">
                <div class="stat-label">MTTR</div>
                <div class="stat-value">
                  {{ formatLT(r.metrics?.mttrMeanSeconds) }}
                </div>
              </div>
            </div>

            @if (i === 0 && r.metrics?.classification !== 'insufficient_data') {
              <div class="badge-top">
                <mat-icon fontIcon="workspace_premium"></mat-icon>
                Liderando
              </div>
            }
            @if (i === rows().length - 1 && rows().length > 1) {
              <div class="badge-growth">
                <mat-icon fontIcon="trending_up"></mat-icon>
                Em crescimento
              </div>
            }
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
        align-items: flex-start;
        gap: var(--space-4);
        margin-bottom: var(--space-5);
        flex-wrap: wrap;
      }
      .head h1 {
        margin: 0 0 var(--space-2);
      }
      .head .muted {
        max-width: 540px;
      }
      .head-actions {
        display: flex;
        gap: var(--space-3);
      }

      .stack {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .leader-card {
        display: grid;
        grid-template-columns: 60px 220px 140px 1fr auto;
        align-items: center;
        gap: var(--space-4);
        padding: var(--space-4) !important;
      }
      .leader-card.skel {
        grid-template-columns: 60px 1fr 1fr 1fr;
      }
      @media (max-width: 880px) {
        .leader-card {
          grid-template-columns: 60px 1fr 1fr;
        }
        .stats { grid-column: 1 / -1; }
      }

      .rank {
        display: flex;
        align-items: baseline;
        justify-content: center;
        font-family: var(--font-mono);
        color: var(--color-text-muted);
      }
      .rank-num {
        font-size: 2rem;
        font-weight: 700;
        color: var(--color-text-secondary);
      }
      .rank-suffix {
        font-size: var(--font-size-sm);
        margin-left: 2px;
      }

      .team {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        padding: var(--space-2) var(--space-3);
        border-radius: var(--radius-md);
        color: white;
      }
      .team-emoji {
        font-size: 22px;
        line-height: 1;
      }
      .team-name {
        font-weight: 600;
        letter-spacing: -0.01em;
      }

      .tier {
        display: flex;
        justify-content: flex-start;
      }

      .stats {
        display: flex;
        gap: var(--space-5);
        flex-wrap: wrap;
      }
      .stat {
        display: flex;
        flex-direction: column;
        gap: 2px;
      }
      .stat-label {
        font-size: var(--font-size-xs);
        font-weight: 600;
        color: var(--color-text-muted);
        text-transform: uppercase;
        letter-spacing: 0.04em;
      }
      .stat-value {
        font-family: var(--font-mono);
        font-size: var(--font-size-sm);
        font-weight: 600;
        color: var(--color-text-primary);
      }

      .badge-top,
      .badge-growth {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        padding: 4px 10px;
        border-radius: 999px;
        font-size: var(--font-size-xs);
        font-weight: 700;
        letter-spacing: 0.02em;
      }
      .badge-top {
        background: var(--color-tier-elite-bg);
        color: var(--color-tier-elite);
      }
      .badge-growth {
        background: var(--color-brand-subtle);
        color: var(--color-brand);
      }
      .badge-top mat-icon,
      .badge-growth mat-icon {
        font-size: 16px;
        height: 16px;
        width: 16px;
      }
    `,
  ],
})
export class LeaderboardComponent {
  private api = inject(ApiClient);

  tenant = 'acme';
  selectedWindow: Window = '30d';

  loading = signal(false);
  teams = signal<Team[]>([]);
  metricsById = signal<Record<string, DoraMetrics | null>>({});

  rows = computed<LeaderRow[]>(() => {
    const m = this.metricsById();
    const list: LeaderRow[] = this.teams().map((t) => ({
      team: t,
      metrics: m[t.id] ?? null,
    }));
    // Rank por tier desc, depois por DF desc, depois alfabético.
    list.sort((a, b) => {
      const ra = TIER_RANK[a.metrics?.classification ?? 'insufficient_data'];
      const rb = TIER_RANK[b.metrics?.classification ?? 'insufficient_data'];
      if (ra !== rb) return rb - ra;
      const da = a.metrics?.deploymentFrequency ?? -1;
      const db = b.metrics?.deploymentFrequency ?? -1;
      if (da !== db) return db - da;
      return a.team.name.localeCompare(b.team.name);
    });
    return list;
  });

  constructor() {
    this.reload();
  }

  reload(): void {
    if (!this.tenant) return;
    this.loading.set(true);
    this.api
      .listTeams(this.tenant)
      .pipe(
        catchError(() => of([] as Team[])),
        finalize(() => {
          // só apaga o spinner quando não houver times — caso contrário,
          // espera o forkJoin de métricas terminar.
          if (this.teams().length === 0) this.loading.set(false);
        }),
      )
      .subscribe((teams) => {
        this.teams.set(teams);
        if (teams.length === 0) {
          this.metricsById.set({});
          return;
        }
        this.fetchAllMetrics(teams);
      });
  }

  private fetchAllMetrics(teams: Team[]): void {
    const requests: Record<string, Observable<DoraMetrics | null>> = {};
    for (const t of teams) {
      requests[t.id] = this.api
        .getTeamMetrics(t.id, this.selectedWindow)
        .pipe(catchError(() => of<DoraMetrics | null>(null)));
    }
    forkJoin(requests)
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe((res) => {
        this.metricsById.set(res as Record<string, DoraMetrics | null>);
      });
  }

  formatLT(seconds: number | null | undefined): string {
    if (seconds === null || seconds === undefined) return '—';
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${(seconds / 60).toFixed(0)}min`;
    if (seconds < 86400) return `${(seconds / 3600).toFixed(1)}h`;
    return `${(seconds / 86400).toFixed(1)}d`;
  }

  formatCFR(rate: number | null | undefined): string {
    if (rate === null || rate === undefined) return '—';
    return `${(rate * 100).toFixed(1)}%`;
  }
}
