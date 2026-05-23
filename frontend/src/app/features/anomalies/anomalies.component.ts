import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { DatePipe } from '@angular/common';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { MatCardModule } from '@angular/material/card';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatChipsModule } from '@angular/material/chips';
import { MatExpansionModule } from '@angular/material/expansion';
import { MatSelectModule } from '@angular/material/select';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatTooltipModule } from '@angular/material/tooltip';
import { FormsModule } from '@angular/forms';
import { catchError, finalize, of } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import { Anomaly, AnomalyMetric, AnomalySeverity } from '../../core/api/api.types';
import { SkeletonComponent } from '../../shared/skeleton.component';
import { EmptyStateComponent } from '../../shared/empty-state.component';
import { ErrorStateComponent } from '../../shared/error-state.component';

/** Mapeia cada métrica para ícone Material e label em PT-BR */
const METRIC_META: Record<AnomalyMetric, { icon: string; label: string }> = {
  df:   { icon: 'rocket_launch', label: 'Deployment Frequency' },
  lt:   { icon: 'timer',         label: 'Lead Time' },
  cfr:  { icon: 'report',        label: 'Change Failure Rate' },
  mttr: { icon: 'healing',       label: 'MTTR' },
};

const SEVERITY_LABEL: Record<AnomalySeverity, string> = {
  warning:  'Alerta',
  critical: 'Crítico',
};

type Window = '7d' | '30d' | '90d';

@Component({
  selector: 'app-anomalies',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    DatePipe,
    RouterLink,
    FormsModule,
    MatCardModule,
    MatButtonModule,
    MatIconModule,
    MatChipsModule,
    MatExpansionModule,
    MatSelectModule,
    MatFormFieldModule,
    MatTooltipModule,
    SkeletonComponent,
    EmptyStateComponent,
    ErrorStateComponent,
  ],
  template: `
    <div class="page-header">
      <div class="breadcrumb">
        <a routerLink="/dashboard" class="breadcrumb-link">
          <mat-icon fontIcon="dashboard" class="breadcrumb-icon"></mat-icon>
          Dashboard
        </a>
        <mat-icon fontIcon="chevron_right" class="breadcrumb-sep"></mat-icon>
        <span>Anomalias</span>
      </div>
      <h1>Anomalias detectadas</h1>
      <p class="page-subtitle">
        Desvios estatísticos nas 4 métricas DORA detectados automaticamente
        pelo sistema de monitoramento contínuo.
      </p>
    </div>

    <div class="filters">
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

    @if (!projectId()) {
      <mat-card appearance="outlined">
        <mat-card-content>
          <app-empty-state
            icon="manage_search"
            title="Nenhum projeto selecionado"
            description="Acesse essa tela a partir de um projeto específico para ver as anomalias."
          >
            <a mat-flat-button color="primary" routerLink="/projects">
              <mat-icon fontIcon="folder"></mat-icon>
              Ver projetos
            </a>
          </app-empty-state>
        </mat-card-content>
      </mat-card>
    } @else if (loading()) {
      <div class="skeleton-list">
        @for (_ of [0, 1, 2, 3]; track $index) {
          <mat-card appearance="outlined" class="skel-card">
            <div class="skel-row">
              <app-skeleton variant="chip" width="80px" />
              <app-skeleton variant="chip" width="120px" />
            </div>
            <app-skeleton variant="title" width="60%" />
            <app-skeleton variant="text" width="80%" />
          </mat-card>
        }
      </div>
    } @else if (error()) {
      <mat-card appearance="outlined">
        <mat-card-content>
          <app-error-state
            variant="network"
            title="Não foi possível carregar as anomalias"
            [description]="error() ?? 'Tente novamente.'"
          >
            <button mat-flat-button color="primary" (click)="reload()">
              <mat-icon fontIcon="refresh"></mat-icon>
              Tentar novamente
            </button>
          </app-error-state>
        </mat-card-content>
      </mat-card>
    } @else if (anomalies().length === 0) {
      <mat-card appearance="outlined">
        <mat-card-content>
          <app-empty-state
            icon="verified"
            title="Nenhuma anomalia detectada"
            description="Suas métricas DORA estão dentro do comportamento esperado para a janela selecionada. Continue assim!"
          />
        </mat-card-content>
      </mat-card>
    } @else {
      <div class="summary-chips">
        <span class="summary-label">
          <mat-icon fontIcon="bar_chart" class="summary-icon"></mat-icon>
          {{ anomalies().length }} anomalia{{ anomalies().length > 1 ? 's' : '' }} encontrada{{ anomalies().length > 1 ? 's' : '' }}
        </span>
        @if (criticalCount() > 0) {
          <mat-chip class="chip-critical">
            <mat-icon fontIcon="error" class="chip-icon"></mat-icon>
            {{ criticalCount() }} crítica{{ criticalCount() > 1 ? 's' : '' }}
          </mat-chip>
        }
        @if (warningCount() > 0) {
          <mat-chip class="chip-warning">
            <mat-icon fontIcon="warning" class="chip-icon"></mat-icon>
            {{ warningCount() }} alerta{{ warningCount() > 1 ? 's' : '' }}
          </mat-chip>
        }
      </div>

      <mat-accordion class="anomaly-list" multi>
        @for (a of anomalies(); track a.id) {
          <mat-expansion-panel class="anomaly-panel" [class.panel-critical]="a.severity === 'critical'" [class.panel-warning]="a.severity === 'warning'">
            <mat-expansion-panel-header>
              <mat-panel-title class="panel-title">
                <mat-icon
                  [fontIcon]="metricMeta(a.metric).icon"
                  class="metric-icon"
                  [matTooltip]="metricMeta(a.metric).label"
                ></mat-icon>
                <span class="panel-label">{{ metricMeta(a.metric).label }}</span>
                <mat-chip
                  class="sev-chip"
                  [class.chip-critical]="a.severity === 'critical'"
                  [class.chip-warning]="a.severity === 'warning'"
                >
                  <mat-icon
                    [fontIcon]="a.severity === 'critical' ? 'error' : 'warning'"
                    class="sev-icon"
                  ></mat-icon>
                  {{ severityLabel(a.severity) }}
                </mat-chip>
              </mat-panel-title>
              <mat-panel-description>
                <span class="panel-date">
                  Detectada em {{ a.detectedAt | date: 'dd/MM/yyyy' }}
                </span>
              </mat-panel-description>
            </mat-expansion-panel-header>

            <div class="panel-body">
              <p class="anomaly-description">{{ a.description }}</p>

              @if (a.valueObserved !== null || a.expectedMin !== null) {
                <div class="value-grid">
                  @if (a.valueObserved !== null) {
                    <div class="value-card">
                      <div class="value-label">Valor observado</div>
                      <div class="value-num">{{ formatValue(a.metric, a.valueObserved) }}</div>
                    </div>
                  }
                  @if (a.expectedMin !== null && a.expectedMax !== null) {
                    <div class="value-card expected">
                      <div class="value-label">Faixa esperada</div>
                      <div class="value-num">
                        {{ formatValue(a.metric, a.expectedMin) }} – {{ formatValue(a.metric, a.expectedMax) }}
                      </div>
                    </div>
                  }
                </div>
              }

              <div class="meta-row">
                <mat-icon fontIcon="date_range" class="meta-icon"></mat-icon>
                <span class="meta-text">
                  Janela: {{ a.windowStart | date: 'dd/MM/yyyy' }} – {{ a.windowEnd | date: 'dd/MM/yyyy' }}
                </span>
              </div>
            </div>
          </mat-expansion-panel>
        }
      </mat-accordion>
    }
  `,
  styles: [
    `
      .page-header {
        margin-bottom: var(--space-5);
      }
      .breadcrumb {
        display: flex;
        align-items: center;
        gap: var(--space-1);
        font-size: var(--font-size-sm);
        color: var(--color-text-muted);
        margin-bottom: var(--space-2);
      }
      .breadcrumb-link {
        display: flex;
        align-items: center;
        gap: 4px;
        color: var(--color-brand);
        text-decoration: none;
      }
      .breadcrumb-link:hover { text-decoration: underline; }
      .breadcrumb-icon,
      .breadcrumb-sep {
        font-size: 16px;
        height: 16px;
        width: 16px;
      }
      .page-header h1 {
        margin: 0 0 var(--space-2);
      }
      .page-subtitle {
        margin: 0;
        color: var(--color-text-secondary);
        font-size: var(--font-size-sm);
        line-height: 1.5;
      }

      .filters {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        margin-bottom: var(--space-4);
      }

      /* Skeleton */
      .skeleton-list {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .skel-card {
        padding: var(--space-4);
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .skel-row {
        display: flex;
        gap: var(--space-2);
      }

      /* Summary chips */
      .summary-chips {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        margin-bottom: var(--space-4);
        flex-wrap: wrap;
      }
      .summary-label {
        display: flex;
        align-items: center;
        gap: var(--space-1);
        font-size: var(--font-size-sm);
        color: var(--color-text-secondary);
        font-weight: 500;
      }
      .summary-icon {
        font-size: 16px;
        height: 16px;
        width: 16px;
      }

      /* Chips */
      .chip-critical {
        background: var(--color-tier-low-bg) !important;
        color: var(--color-tier-low) !important;
      }
      .chip-warning {
        background: var(--color-tier-medium-bg) !important;
        color: var(--color-tier-medium) !important;
      }
      .chip-icon,
      .sev-icon {
        font-size: 14px !important;
        height: 14px !important;
        width: 14px !important;
        margin-right: 4px !important;
      }

      /* Accordion */
      .anomaly-list {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
      }
      .anomaly-panel {
        border: 1px solid var(--color-border) !important;
        border-radius: var(--radius-lg) !important;
        box-shadow: none !important;
      }
      .anomaly-panel.panel-critical {
        border-left: 4px solid var(--color-tier-low) !important;
      }
      .anomaly-panel.panel-warning {
        border-left: 4px solid var(--color-tier-medium) !important;
      }

      .panel-title {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        font-weight: 600;
        color: var(--color-text-primary);
      }
      .metric-icon {
        font-size: 20px;
        height: 20px;
        width: 20px;
        color: var(--color-brand);
      }
      .panel-label {
        flex: 1;
      }
      .sev-chip {
        display: flex;
        align-items: center;
        font-size: var(--font-size-xs) !important;
        height: 22px !important;
        padding: 0 8px !important;
        min-height: unset !important;
      }
      .panel-date {
        font-size: var(--font-size-xs);
        color: var(--color-text-muted);
      }

      .panel-body {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
        padding: var(--space-2) 0 var(--space-2);
      }
      .anomaly-description {
        margin: 0;
        color: var(--color-text-secondary);
        line-height: 1.5;
      }

      .value-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
        gap: var(--space-3);
      }
      .value-card {
        padding: var(--space-3);
        border-radius: var(--radius-md);
        background: var(--color-tier-low-bg);
        border: 1px solid color-mix(in srgb, var(--color-tier-low) 20%, transparent);
      }
      .value-card.expected {
        background: var(--color-surface-subtle);
        border-color: var(--color-border);
      }
      .value-label {
        font-size: var(--font-size-xs);
        color: var(--color-text-muted);
        font-weight: 500;
        text-transform: uppercase;
        letter-spacing: 0.04em;
        margin-bottom: 4px;
      }
      .value-num {
        font-size: var(--font-size-lg);
        font-weight: 700;
        color: var(--color-text-primary);
        font-variant-numeric: tabular-nums;
      }

      .meta-row {
        display: flex;
        align-items: center;
        gap: var(--space-1);
        font-size: var(--font-size-xs);
        color: var(--color-text-muted);
      }
      .meta-icon {
        font-size: 14px;
        height: 14px;
        width: 14px;
      }
    `,
  ],
})
export class AnomaliesComponent {
  private api = inject(ApiClient);
  private route = inject(ActivatedRoute);

  selectedWindow: Window = '90d';
  loading = signal(false);
  error = signal<string | null>(null);
  anomalies = signal<Anomaly[]>([]);

  projectId = computed(() => this.route.snapshot.paramMap.get('id'));

  criticalCount = computed(() =>
    this.anomalies().filter((a) => a.severity === 'critical').length,
  );
  warningCount = computed(() =>
    this.anomalies().filter((a) => a.severity === 'warning').length,
  );

  constructor() {
    if (this.projectId()) {
      this.reload();
    }
  }

  reload(): void {
    const id = this.projectId();
    if (!id) return;
    this.loading.set(true);
    this.error.set(null);
    this.api.listProjectAnomalies(id, this.selectedWindow).pipe(
      catchError((err) => {
        this.error.set(err?.error?.message ?? err?.message ?? 'Erro ao carregar anomalias');
        return of([] as Anomaly[]);
      }),
      finalize(() => this.loading.set(false)),
    ).subscribe((rows) => this.anomalies.set(rows));
  }

  metricMeta(metric: AnomalyMetric) {
    return METRIC_META[metric] ?? { icon: 'analytics', label: metric };
  }

  severityLabel(severity: AnomalySeverity): string {
    return SEVERITY_LABEL[severity] ?? severity;
  }

  formatValue(metric: AnomalyMetric, value: number | null): string {
    if (value === null) return '—';
    switch (metric) {
      case 'df':
        return `${value.toFixed(1)} deploy/dia`;
      case 'lt': {
        const h = Math.round(value / 3600);
        return h >= 24 ? `${Math.round(h / 24)}d` : `${h}h`;
      }
      case 'cfr':
        return `${(value * 100).toFixed(1)}%`;
      case 'mttr': {
        const h = Math.round(value / 3600);
        return h >= 24 ? `${Math.round(h / 24)}d` : `${h}h`;
      }
      default:
        return String(value);
    }
  }
}
