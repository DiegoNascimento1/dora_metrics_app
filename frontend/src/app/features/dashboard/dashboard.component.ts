import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatSelectModule } from '@angular/material/select';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { forkJoin, of, catchError, finalize } from 'rxjs';

import { SkeletonComponent } from '../../shared/skeleton.component';
import { EmptyStateComponent } from '../../shared/empty-state.component';

import { ApiClient } from '../../core/api/api.client';
import {
  Classification,
  Deployment,
  DoraMetrics,
  Project,
  ProjectAchievements,
  Team,
  TimeseriesPoint,
} from '../../core/api/api.types';
import { TimeseriesChartComponent } from './timeseries-chart.component';
import { DeploymentsTableComponent } from './deployments-table.component';
import { AchievementsCardComponent } from './achievements-card.component';

interface MetricTile {
  label: string;
  value: string;
  classification: Classification;
}

type Window = '7d' | '30d' | '90d';

@Component({
  selector: 'app-dashboard',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    RouterLink,
    MatCardModule,
    MatChipsModule,
    MatSelectModule,
    MatFormFieldModule,
    MatButtonModule,
    MatIconModule,
    SkeletonComponent,
    EmptyStateComponent,
    TimeseriesChartComponent,
    DeploymentsTableComponent,
    AchievementsCardComponent,
  ],
  template: `
    <h1>DORA — visão geral</h1>

    @if (projects().length === 0 && !loading()) {
      <mat-card appearance="outlined">
        <mat-card-content>
          <app-empty-state
            icon="rocket_launch"
            title="Bem-vindo ao DORA Metrics"
            description="Primeiro passo: conecte uma instância GitLab/Jira e cadastre um projeto para começar a ver suas 4 métricas em ação."
          >
            <a mat-flat-button color="primary" routerLink="/settings">
              <mat-icon fontIcon="cable"></mat-icon> Conectar GitLab/Jira
            </a>
          </app-empty-state>
        </mat-card-content>
      </mat-card>
    } @else {
      <div class="filters">
        <mat-form-field appearance="outline">
          <mat-label>Escopo</mat-label>
          <mat-select [(value)]="scope" (selectionChange)="onScopeChange()">
            <mat-option value="project">Por projeto</mat-option>
            <mat-option value="team" [disabled]="teams().length === 0">
              Por time
            </mat-option>
          </mat-select>
        </mat-form-field>

        @if (scope === 'project') {
          <mat-form-field appearance="outline">
            <mat-label>Projeto</mat-label>
            <mat-select [(value)]="selectedProjectId" (selectionChange)="reload()">
              @for (p of projects(); track p.id) {
                <mat-option [value]="p.id">{{ p.pathWithNamespace }}</mat-option>
              }
            </mat-select>
          </mat-form-field>
        } @else {
          <mat-form-field appearance="outline">
            <mat-label>Time</mat-label>
            <mat-select [(value)]="selectedTeamId" (selectionChange)="reload()">
              @for (t of teams(); track t.id) {
                <mat-option [value]="t.id">
                  {{ t.emoji || '👥' }} {{ t.name }}
                </mat-option>
              }
            </mat-select>
          </mat-form-field>
        }

        <mat-form-field appearance="outline">
          <mat-label>Janela</mat-label>
          <mat-select [(value)]="selectedWindow" (selectionChange)="reload()">
            <mat-option value="7d">7 dias</mat-option>
            <mat-option value="30d">30 dias</mat-option>
            <mat-option value="90d">90 dias</mat-option>
          </mat-select>
        </mat-form-field>

        <button mat-stroked-button (click)="reload()">
          <mat-icon fontIcon="refresh"></mat-icon>
          Atualizar
        </button>
      </div>

      @if (loading()) {
        <div class="grid">
          @for (_ of [0, 1, 2, 3]; track $index) {
            <mat-card appearance="outlined" class="skel-tile">
              <app-skeleton variant="text" width="60%" />
              <app-skeleton variant="title" width="40%" />
              <app-skeleton variant="chip" width="80px" />
            </mat-card>
          }
        </div>
        <mat-card appearance="outlined" class="chart-card">
          <app-skeleton variant="text" width="40%" />
          <app-skeleton variant="card" height="200px" />
        </mat-card>
      } @else if (error()) {
        <mat-card appearance="outlined" class="error">
          <mat-card-content>{{ error() }}</mat-card-content>
        </mat-card>
      } @else {
        <div class="grid">
          @for (tile of tiles(); track tile.label) {
            <mat-card appearance="outlined">
              <mat-card-header>
                <mat-card-title>{{ tile.label }}</mat-card-title>
              </mat-card-header>
              <mat-card-content>
                <div class="value">{{ tile.value }}</div>
                <mat-chip [class]="'tier-' + tile.classification">
                  {{ tile.classification }}
                </mat-chip>
              </mat-card-content>
            </mat-card>
          }
        </div>

        @if (scope === 'project') {
          <app-achievements-card [data]="achievements()" />
        }

        <mat-card appearance="outlined" class="chart-card">
          <mat-card-header>
            <mat-card-title>
              {{ scope === 'team' ? 'Deploys do time por dia' : 'Deploys de produção por dia' }}
            </mat-card-title>
          </mat-card-header>
          <mat-card-content>
            @if (points().length === 0) {
              <p class="empty">Sem deploys na janela.</p>
            } @else {
              <app-timeseries-chart [points]="points()" />
            }
          </mat-card-content>
        </mat-card>

        @if (scope === 'project') {
          <mat-card appearance="outlined" class="table-card">
            <mat-card-header>
              <mat-card-title>
                Drill-down — {{ deployments().length }} deploys na janela
              </mat-card-title>
            </mat-card-header>
            <mat-card-content>
              @if (deployments().length === 0) {
                <p class="empty">Sem deploys.</p>
              } @else {
                <app-deployments-table [deployments]="deployments()" />
              }
            </mat-card-content>
          </mat-card>
        }

        <p class="meta">
          Amostra: {{ metrics()?.sampleSize ?? 0 }} deploys ·
          Calculado: {{ metrics()?.computedAt ?? '—' }}
        </p>
      }
    }
  `,
  styles: [
    `
      .filters {
        display: flex;
        gap: 16px;
        margin: 16px 0;
        align-items: center;
      }
      .grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
        gap: 16px;
        margin-top: 16px;
      }
      .value {
        font-size: 2rem;
        font-weight: 500;
        margin: 12px 0;
      }
      .meta {
        margin-top: 16px;
        color: #666;
        font-size: 0.875rem;
      }
      .error {
        background: #fff3e0;
      }
      .chart-card,
      .table-card {
        margin-top: 24px;
      }
      .skel-tile {
        padding: var(--space-4);
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .chart-card app-skeleton {
        display: block;
        margin: var(--space-3) 0;
      }
      .empty {
        color: #888;
        margin: 16px 0;
      }
      /* tier-* classes vêm dos estilos globais (src/styles/_tier-badge.scss) */
    `,
  ],
})
export class DashboardComponent {
  private api = inject(ApiClient);

  loading = signal(false);
  error = signal<string | null>(null);
  projects = signal<Project[]>([]);
  teams = signal<Team[]>([]);
  metrics = signal<DoraMetrics | null>(null);
  points = signal<TimeseriesPoint[]>([]);
  deployments = signal<Deployment[]>([]);
  achievements = signal<ProjectAchievements | null>(null);

  scope: 'project' | 'team' = 'project';
  selectedProjectId: string | null = null;
  selectedTeamId: string | null = null;
  selectedWindow: Window = '30d';

  tiles = computed<MetricTile[]>(() => {
    const m = this.metrics();
    if (!m) {
      return [];
    }
    return [
      {
        label: 'Deployment Frequency',
        value: `${m.deploymentFrequency.toFixed(2)}/dia`,
        classification: m.classification,
      },
      {
        label: 'Lead Time (mediana)',
        value: this.formatDuration(m.leadTimeMedianSeconds),
        classification: m.classification,
      },
      {
        label: 'Change Failure Rate',
        value:
          m.changeFailureRate === null
            ? '—'
            : `${(m.changeFailureRate * 100).toFixed(1)}%`,
        classification: m.classification,
      },
      {
        label: 'MTTR (média)',
        value: this.formatDuration(m.mttrMeanSeconds),
        classification: m.classification,
      },
    ];
  });

  constructor() {
    this.loadProjects();
  }

  private loadProjects(): void {
    this.loading.set(true);
    forkJoin({
      projects: this.api.listProjects().pipe(
        catchError((err) => {
          this.error.set(this.errorMessage(err));
          return of([] as Project[]);
        }),
      ),
      teams: this.api
        .listTeams('acme')
        .pipe(catchError(() => of([] as Team[]))),
    })
      .pipe(
        finalize(() => {
          if (this.projects().length === 0 && this.teams().length === 0) {
            this.loading.set(false);
          }
        }),
      )
      .subscribe(({ projects, teams }) => {
        this.projects.set(projects);
        this.teams.set(teams);
        if (projects.length > 0) {
          this.selectedProjectId = projects[0].id;
          this.reload();
        }
      });
  }

  onScopeChange(): void {
    if (this.scope === 'team' && !this.selectedTeamId && this.teams().length > 0) {
      this.selectedTeamId = this.teams()[0].id;
    }
    if (this.scope === 'project' && !this.selectedProjectId && this.projects().length > 0) {
      this.selectedProjectId = this.projects()[0].id;
    }
    this.reload();
  }

  reload(): void {
    this.error.set(null);
    if (this.scope === 'team') {
      if (!this.selectedTeamId) return;
      this.reloadTeam(this.selectedTeamId);
    } else {
      if (!this.selectedProjectId) return;
      this.reloadProject(this.selectedProjectId);
    }
  }

  private reloadProject(projectId: string): void {
    this.loading.set(true);
    forkJoin({
      metrics: this.api
        .getProjectMetrics(projectId, this.selectedWindow)
        .pipe(catchError(() => of(null))),
      timeseries: this.api
        .getProjectTimeseries(projectId, this.selectedWindow)
        .pipe(catchError(() => of({ points: [] as TimeseriesPoint[] }))),
      deployments: this.api
        .listProjectDeployments(projectId, this.selectedWindow)
        .pipe(catchError(() => of([] as Deployment[]))),
      achievements: this.api
        .getProjectAchievements(projectId, this.selectedWindow)
        .pipe(catchError(() => of<ProjectAchievements | null>(null))),
    })
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe(({ metrics, timeseries, deployments, achievements }) => {
        this.metrics.set(metrics);
        this.points.set(timeseries.points ?? []);
        this.deployments.set(deployments);
        this.achievements.set(achievements);
      });
  }

  private reloadTeam(teamId: string): void {
    this.loading.set(true);
    forkJoin({
      metrics: this.api
        .getTeamMetrics(teamId, this.selectedWindow)
        .pipe(catchError(() => of(null))),
      timeseries: this.api
        .getTeamTimeseries(teamId, this.selectedWindow)
        .pipe(catchError(() => of({ points: [] as TimeseriesPoint[] }))),
    })
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe(({ metrics, timeseries }) => {
        this.metrics.set(metrics);
        this.points.set(timeseries.points ?? []);
        // Achievements e drill-down ainda não suportam scope=team.
        this.achievements.set(null);
        this.deployments.set([]);
      });
  }

  private formatDuration(seconds: number | null | undefined): string {
    if (seconds === null || seconds === undefined) return '—';
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${(seconds / 60).toFixed(0)}min`;
    if (seconds < 86400) return `${(seconds / 3600).toFixed(1)}h`;
    return `${(seconds / 86400).toFixed(1)}d`;
  }

  private errorMessage(err: unknown): string {
    if (err instanceof Error) return err.message;
    return 'Erro ao carregar métricas';
  }
}
