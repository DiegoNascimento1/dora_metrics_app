import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { MatSelectModule } from '@angular/material/select';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { FormsModule } from '@angular/forms';
import { forkJoin, of, catchError, finalize } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import {
  Classification,
  Deployment,
  DoraMetrics,
  Project,
  ProjectAchievements,
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
    MatCardModule,
    MatChipsModule,
    MatProgressSpinnerModule,
    MatSelectModule,
    MatFormFieldModule,
    MatButtonModule,
    MatIconModule,
    TimeseriesChartComponent,
    DeploymentsTableComponent,
    AchievementsCardComponent,
  ],
  template: `
    <h1>DORA — visão geral</h1>

    @if (projects().length === 0 && !loading()) {
      <mat-card appearance="outlined">
        <mat-card-content>
          Nenhum projeto cadastrado ainda. Use a CLI para adicionar:
          <pre>
docker compose run --rm cli project add \\
  --tenant acme --source gitlab-prod \\
  --external-id 123 --path acme/api
          </pre>
        </mat-card-content>
      </mat-card>
    } @else {
      <div class="filters">
        <mat-form-field appearance="outline">
          <mat-label>Projeto</mat-label>
          <mat-select [(value)]="selectedProjectId" (selectionChange)="reload()">
            @for (p of projects(); track p.id) {
              <mat-option [value]="p.id">{{ p.pathWithNamespace }}</mat-option>
            }
          </mat-select>
        </mat-form-field>

        <mat-form-field appearance="outline">
          <mat-label>Janela</mat-label>
          <mat-select [(value)]="selectedWindow" (selectionChange)="reload()">
            <mat-option value="7d">7 dias</mat-option>
            <mat-option value="30d">30 dias</mat-option>
            <mat-option value="90d">90 dias</mat-option>
          </mat-select>
        </mat-form-field>

        <button mat-stroked-button (click)="reload()">
          <mat-icon>refresh</mat-icon>
          Atualizar
        </button>
      </div>

      @if (loading()) {
        <mat-progress-spinner mode="indeterminate" diameter="40" />
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

        <app-achievements-card [data]="achievements()" />

        <mat-card appearance="outlined" class="chart-card">
          <mat-card-header>
            <mat-card-title>Deploys de produção por dia</mat-card-title>
          </mat-card-header>
          <mat-card-content>
            @if (points().length === 0) {
              <p class="empty">Sem deploys na janela.</p>
            } @else {
              <app-timeseries-chart [points]="points()" />
            }
          </mat-card-content>
        </mat-card>

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
  metrics = signal<DoraMetrics | null>(null);
  points = signal<TimeseriesPoint[]>([]);
  deployments = signal<Deployment[]>([]);
  achievements = signal<ProjectAchievements | null>(null);

  selectedProjectId: string | null = null;
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
    this.api
      .listProjects()
      .pipe(
        catchError((err) => {
          this.error.set(this.errorMessage(err));
          return of([] as Project[]);
        }),
        finalize(() => {
          if (this.projects().length === 0) this.loading.set(false);
        }),
      )
      .subscribe((projects) => {
        this.projects.set(projects);
        if (projects.length > 0) {
          this.selectedProjectId = projects[0].id;
          this.reload();
        }
      });
  }

  reload(): void {
    if (!this.selectedProjectId) return;
    this.loading.set(true);
    this.error.set(null);

    forkJoin({
      metrics: this.api
        .getProjectMetrics(this.selectedProjectId, this.selectedWindow)
        .pipe(catchError(() => of(null))),
      timeseries: this.api
        .getProjectTimeseries(this.selectedProjectId, this.selectedWindow)
        .pipe(catchError(() => of({ points: [] as TimeseriesPoint[] }))),
      deployments: this.api
        .listProjectDeployments(this.selectedProjectId, this.selectedWindow)
        .pipe(catchError(() => of([] as Deployment[]))),
      achievements: this.api
        .getProjectAchievements(this.selectedProjectId, this.selectedWindow)
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
