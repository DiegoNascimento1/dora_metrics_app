import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  OnDestroy,
  signal,
} from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatSelectModule } from '@angular/material/select';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';
import { MatTooltipModule } from '@angular/material/tooltip';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatDialog, MatDialogModule } from '@angular/material/dialog';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { forkJoin, of, catchError, finalize } from 'rxjs';

import { DemoService } from '../../core/demo/demo.service';

import { SkeletonComponent } from '../../shared/skeleton.component';
import { EmptyStateComponent } from '../../shared/empty-state.component';
import { ErrorStateComponent } from '../../shared/error-state.component';
import {
  TierExplainDialogComponent,
  TierExplainData,
} from '../../shared/tier-explain-dialog.component';
import {
  MetricKey,
  NextTierProgress,
  formatMetricValue,
  nextTierProgress,
} from '../../shared/dora-tiers';

import { ApiClient } from '../../core/api/api.client';
import {
  Classification,
  Deployment,
  DoraMetrics,
  PredictResponse,
  Project,
  ProjectAchievements,
  Team,
  TimeseriesPoint,
} from '../../core/api/api.types';
import { TimeseriesChartComponent } from './timeseries-chart.component';
import { DeploymentsTableComponent } from './deployments-table.component';
import { AchievementsCardComponent } from './achievements-card.component';
import { WeeklyDigestCardComponent } from './weekly-digest-card.component';

interface MetricTile {
  key: MetricKey;
  label: string;
  /** Texto explicativo curto exibido no tooltip do ícone "info". */
  hint: string;
  value: string;
  classification: Classification;
  progress: NextTierProgress;
}

/**
 * Documentação ancorada por métrica em docs/01-dora-metrics.md.
 * Mantém o link relativo do repo (GitHub renderiza markdown).
 */
const DOCS_URL =
  'https://github.com/diegonascimentoo/dora_metrics_app/blob/main/docs/01-dora-metrics.md';

const METRIC_HINTS: Record<MetricKey, string> = {
  df: 'Deployment Frequency — quantos deploys de produção saíram por dia na janela.',
  lt: 'Lead Time for Changes — mediana de tempo entre o 1º commit do MR e o deploy em produção.',
  cfr: 'Change Failure Rate — % de deploys seguidos por incident em até 24h.',
  mttr: 'Mean Time to Recovery — tempo médio entre abertura e resolução de um incident.',
};

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
    MatMenuModule,
    MatTooltipModule,
    MatProgressBarModule,
    MatDialogModule,
    SkeletonComponent,
    EmptyStateComponent,
    ErrorStateComponent,
    TimeseriesChartComponent,
    DeploymentsTableComponent,
    AchievementsCardComponent,
    WeeklyDigestCardComponent,
  ],
  template: `
    <!-- Banner modo demo -->
    @if (demo.isDemo()) {
      <div class="demo-banner">
        <mat-icon fontIcon="science" class="demo-icon"></mat-icon>
        <span>Modo demonstração — dados sintéticos.</span>
        <a routerLink="/settings" class="demo-link">Conectar dados reais →</a>
      </div>
    }

    <!-- Badge SSE status -->
    <div class="sse-status" [class.sse-live]="sseLive()" [class.sse-paused]="!sseLive() && sseConnected()">
      @if (sseLive()) {
        <span class="sse-dot live"></span>
        <span class="sse-label">Ao vivo</span>
      } @else if (sseConnected()) {
        <span class="sse-dot paused"></span>
        <span class="sse-label">Atualização automática pausada</span>
      }
    </div>

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

        <button mat-stroked-button (click)="reload()" class="no-print">
          <mat-icon fontIcon="refresh"></mat-icon>
          Atualizar
        </button>

        <button
          mat-stroked-button
          (click)="printReport()"
          class="no-print"
          matTooltip="Abre o diálogo de impressão do navegador — escolha 'Salvar como PDF' para gerar o relatório"
        >
          <mat-icon fontIcon="print"></mat-icon>
          Imprimir / PDF
        </button>

        @if (scope === 'project' && selectedProjectId) {
          <button
            mat-stroked-button
            [matMenuTriggerFor]="exportMenu"
            matTooltip="Baixar dump bruto da janela"
          >
            <mat-icon fontIcon="download"></mat-icon>
            Exportar
          </button>
          <mat-menu #exportMenu="matMenu">
            <button mat-menu-item [matMenuTriggerFor]="exportDeploy">
              <mat-icon fontIcon="rocket_launch"></mat-icon>
              Deployments
            </button>
            <button mat-menu-item [matMenuTriggerFor]="exportIncidents">
              <mat-icon fontIcon="report"></mat-icon>
              Incidents
            </button>
            <button mat-menu-item [matMenuTriggerFor]="exportMRs">
              <mat-icon fontIcon="merge"></mat-icon>
              Merge Requests
            </button>
          </mat-menu>
          <mat-menu #exportDeploy="matMenu">
            <a mat-menu-item [href]="exportUrl('deployments', 'csv')" download>CSV</a>
            <a mat-menu-item [href]="exportUrl('deployments', 'json')" download>JSON</a>
          </mat-menu>
          <mat-menu #exportIncidents="matMenu">
            <a mat-menu-item [href]="exportUrl('incidents', 'csv')" download>CSV</a>
            <a mat-menu-item [href]="exportUrl('incidents', 'json')" download>JSON</a>
          </mat-menu>
          <mat-menu #exportMRs="matMenu">
            <a mat-menu-item [href]="exportUrl('merge_requests', 'csv')" download>CSV</a>
            <a mat-menu-item [href]="exportUrl('merge_requests', 'json')" download>JSON</a>
          </mat-menu>
        }
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
        <mat-card appearance="outlined">
          <mat-card-content>
            <app-error-state
              variant="network"
              title="Não foi possível carregar as métricas"
              [description]="error() || 'Verifique sua conexão e tente novamente.'"
            >
              <button mat-flat-button color="primary" (click)="reload()">
                <mat-icon fontIcon="refresh"></mat-icon>
                Tentar novamente
              </button>
            </app-error-state>
          </mat-card-content>
        </mat-card>
      } @else {
        <div class="grid">
          @for (tile of tiles(); track tile.label) {
            <mat-card appearance="outlined" class="tile">
              <mat-card-header>
                <mat-card-title>
                  {{ tile.label }}
                  <mat-icon
                    class="info"
                    fontIcon="info"
                    [matTooltip]="tile.hint + ' Saiba mais em docs/01-dora-metrics.md'"
                    matTooltipPosition="above"
                    tabindex="0"
                    [attr.aria-label]="'Sobre ' + tile.label + ': ' + tile.hint"
                  ></mat-icon>
                </mat-card-title>
              </mat-card-header>
              <mat-card-content>
                <div class="value">{{ tile.value }}</div>
                <button
                  type="button"
                  class="tier-chip-btn"
                  (click)="openTierExplain()"
                  [attr.aria-label]="'Por que tier ' + tile.classification + '? Abre painel explicativo.'"
                >
                  <mat-chip
                    [class]="'tier-' + tile.classification"
                    matTooltip="Clique para ver por que esse tier"
                  >
                    {{ tile.classification }}
                  </mat-chip>
                </button>
                @if (tile.progress.next) {
                  <div class="next-tier">
                    <mat-progress-bar
                      mode="determinate"
                      [value]="tile.progress.progress * 100"
                      [attr.aria-label]="'Progresso até o próximo tier: ' + tile.progress.label"
                    ></mat-progress-bar>
                    <small class="next-tier-label">{{ tile.progress.label }}</small>
                  </div>
                } @else if (tile.progress.current === 'elite') {
                  <small class="next-tier-label top">🏆 Você está no topo</small>
                }
              </mat-card-content>
            </mat-card>
          }
        </div>

        <a class="docs-link no-print" [href]="docsUrl" target="_blank" rel="noopener">
          <mat-icon fontIcon="menu_book"></mat-icon>
          Documentação das métricas (docs/01-dora-metrics.md)
        </a>

        @if (scope === 'project') {
          <app-achievements-card [data]="achievements()" />
        }

        <!-- Card: Narrativa IA do período -->
        @if (scope === 'project' && selectedProjectId) {
          <mat-card appearance="outlined" class="ai-card">
            <mat-card-header>
              <mat-card-title>
                <mat-icon fontIcon="auto_awesome" class="ai-icon"></mat-icon>
                Análise do período
              </mat-card-title>
              <mat-card-subtitle>Gerado por IA com base nos últimos 30 dias</mat-card-subtitle>
            </mat-card-header>
            <mat-card-content>
              @if (loadingNarrative()) {
                <div class="narrative-skel">
                  <app-skeleton variant="text" width="95%" />
                  <app-skeleton variant="text" width="80%" />
                  <app-skeleton variant="text" width="60%" />
                </div>
              } @else if (narrativeText()) {
                <p class="narrative-text">{{ narrativeText() }}</p>
              } @else {
                <div class="narrative-empty">
                  <mat-icon fontIcon="key" class="key-icon"></mat-icon>
                  <span>Configure <code>ANTHROPIC_API_KEY</code> para ativar análise por IA.</span>
                </div>
              }
            </mat-card-content>
          </mat-card>

          <!-- Link para Anomalias -->
          <div class="anomalies-link-row">
            <a
              mat-stroked-button
              [routerLink]="['/projects', selectedProjectId, 'anomalies']"
              class="anomalies-link"
            >
              <mat-icon fontIcon="manage_search"></mat-icon>
              Ver anomalias detectadas
            </a>
          </div>
        }

        <app-weekly-digest-card
          [scopeKind]="scope"
          [scopeId]="scope === 'team' ? selectedTeamId : selectedProjectId"
        />

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
      .tile mat-card-title {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
      }
      .info {
        font-size: 16px !important;
        height: 16px !important;
        width: 16px !important;
        color: var(--color-text-muted);
        cursor: help;
        opacity: 0.7;
      }
      .info:hover,
      .info:focus-visible {
        opacity: 1;
      }
      .tier-chip-btn {
        background: none;
        border: none;
        padding: 0;
        cursor: pointer;
        font: inherit;
      }
      .tier-chip-btn:focus-visible {
        outline: 2px solid var(--color-brand);
        outline-offset: 2px;
        border-radius: var(--radius-sm);
      }
      .next-tier {
        margin-top: var(--space-3);
      }
      .next-tier mat-progress-bar {
        height: 4px;
        border-radius: 2px;
      }
      .next-tier-label {
        display: block;
        margin-top: var(--space-1);
        font-size: var(--font-size-xs);
        color: var(--color-text-secondary);
        font-variant-numeric: tabular-nums;
      }
      .next-tier-label.top {
        color: var(--color-tier-elite);
        font-weight: 600;
      }
      .docs-link {
        display: inline-flex;
        align-items: center;
        gap: var(--space-1);
        margin-top: var(--space-4);
        color: var(--color-brand);
        font-size: var(--font-size-sm);
        text-decoration: none;
      }
      .docs-link:hover {
        text-decoration: underline;
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

      /* ---- Demo mode banner ---- */
      .demo-banner {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        background: #fffbeb;
        border: 1px solid #fde68a;
        border-radius: var(--radius-md);
        padding: var(--space-2) var(--space-4);
        margin-bottom: var(--space-3);
        font-size: var(--font-size-sm);
        color: #92400e;
      }
      [data-theme='dark'] .demo-banner {
        background: #451a03;
        border-color: #78350f;
        color: #fde68a;
      }
      .demo-icon {
        font-size: 18px;
        height: 18px;
        width: 18px;
        flex-shrink: 0;
      }
      .demo-link {
        color: var(--color-brand);
        font-weight: 600;
        text-decoration: none;
        margin-left: auto;
      }
      .demo-link:hover { text-decoration: underline; }

      /* ---- SSE status badge ---- */
      .sse-status {
        display: flex;
        align-items: center;
        gap: var(--space-1);
        margin-bottom: var(--space-1);
        font-size: var(--font-size-xs);
        color: var(--color-text-muted);
        min-height: 20px;
      }
      .sse-dot {
        display: inline-block;
        width: 8px;
        height: 8px;
        border-radius: 50%;
        flex-shrink: 0;
      }
      .sse-dot.live {
        background: var(--color-tier-elite);
        animation: pulse-dot 2s ease-in-out infinite;
      }
      .sse-dot.paused {
        background: var(--color-text-muted);
      }
      .sse-label { font-weight: 500; }
      .sse-live .sse-label { color: var(--color-tier-elite); }
      @keyframes pulse-dot {
        0%, 100% { opacity: 1; }
        50%       { opacity: 0.4; }
      }

      /* ---- AI narrative card ---- */
      .ai-card {
        margin-top: var(--space-5);
      }
      .ai-card mat-card-title {
        display: flex;
        align-items: center;
        gap: var(--space-2);
      }
      .ai-icon {
        font-size: 20px;
        height: 20px;
        width: 20px;
        color: var(--color-brand);
      }
      .narrative-skel {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        padding: var(--space-2) 0;
      }
      .narrative-text {
        margin: 0;
        color: var(--color-text-secondary);
        line-height: 1.7;
        font-size: var(--font-size-sm);
      }
      .narrative-empty {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        padding: var(--space-2) var(--space-3);
        background: var(--color-surface-subtle);
        border-radius: var(--radius-md);
        color: var(--color-text-muted);
        font-size: var(--font-size-sm);
      }
      .key-icon {
        font-size: 16px;
        height: 16px;
        width: 16px;
        flex-shrink: 0;
      }
      .narrative-empty code {
        background: var(--color-bg-elevated);
        padding: 1px 6px;
        border-radius: var(--radius-sm);
        font-family: var(--font-mono);
        font-size: 0.85em;
        border: 1px solid var(--color-border);
      }

      /* ---- Anomalies link ---- */
      .anomalies-link-row {
        margin-top: var(--space-3);
        display: flex;
        justify-content: flex-end;
      }
    `,
  ],
})
export class DashboardComponent implements OnDestroy {
  private api = inject(ApiClient);
  private dialog = inject(MatDialog);
  protected demo = inject(DemoService);

  loading = signal(false);
  error = signal<string | null>(null);
  projects = signal<Project[]>([]);
  teams = signal<Team[]>([]);
  metrics = signal<DoraMetrics | null>(null);
  points = signal<TimeseriesPoint[]>([]);
  deployments = signal<Deployment[]>([]);
  achievements = signal<ProjectAchievements | null>(null);

  // ---- SSE state ----
  private metricsStream?: EventSource;
  private sseRetryCount = 0;
  private readonly SSE_MAX_RETRIES = 5;
  sseLive = signal(false);
  sseConnected = signal(false);

  // ---- AI narrative ----
  narrativeText = signal<string | null>(null);
  loadingNarrative = signal(false);

  scope: 'project' | 'team' = 'project';
  selectedProjectId: string | null = null;
  selectedTeamId: string | null = null;
  selectedWindow: Window = '30d';

  /** URL público da documentação das métricas. Usado no link "saiba mais". */
  protected docsUrl = DOCS_URL;

  tiles = computed<MetricTile[]>(() => {
    const m = this.metrics();
    if (!m) {
      return [];
    }
    return [
      {
        key: 'df',
        label: 'Deployment Frequency',
        hint: METRIC_HINTS.df,
        value: formatMetricValue('df', m.deploymentFrequency),
        classification: m.classification,
        progress: nextTierProgress('df', m.deploymentFrequency),
      },
      {
        key: 'lt',
        label: 'Lead Time (mediana)',
        hint: METRIC_HINTS.lt,
        value: formatMetricValue('lt', m.leadTimeMedianSeconds),
        classification: m.classification,
        progress: nextTierProgress('lt', m.leadTimeMedianSeconds),
      },
      {
        key: 'cfr',
        label: 'Change Failure Rate',
        hint: METRIC_HINTS.cfr,
        value: formatMetricValue('cfr', m.changeFailureRate),
        classification: m.classification,
        progress: nextTierProgress('cfr', m.changeFailureRate),
      },
      {
        key: 'mttr',
        label: 'MTTR (média)',
        hint: METRIC_HINTS.mttr,
        value: formatMetricValue('mttr', m.mttrMeanSeconds),
        classification: m.classification,
        progress: nextTierProgress('mttr', m.mttrMeanSeconds),
      },
    ];
  });

  /** Abre o painel "Por que esse tier?" com os 4 valores + cutoffs. */
  openTierExplain(): void {
    const m = this.metrics();
    if (!m) return;
    const scopeLabel = this.scope === 'team'
      ? `Time: ${this.teams().find((t) => t.id === this.selectedTeamId)?.name ?? '—'}`
      : `Projeto: ${this.projects().find((p) => p.id === this.selectedProjectId)?.pathWithNamespace ?? '—'}`;
    this.dialog.open<TierExplainDialogComponent, TierExplainData>(
      TierExplainDialogComponent,
      {
        data: { metrics: m, combined: m.classification, scopeLabel },
        maxWidth: '640px',
        width: '100%',
        autoFocus: 'dialog',
      },
    );
  }

  /** Dispara o diálogo de impressão do navegador (Salvar como PDF). */
  printReport(): void {
    window.print();
  }

  constructor() {
    // Ativa modo demo se ?demo=true na URL
    this.demo.init();
    this.loadProjects();
  }

  ngOnDestroy(): void {
    this.closeSSE();
  }

  // ---- SSE consumer ----

  private openSSE(projectId: string): void {
    this.closeSSE();
    this.sseRetryCount = 0;
    this.connectSSE(projectId);
  }

  private connectSSE(projectId: string): void {
    const url = `/api/v1/projects/${projectId}/metrics/stream`;
    const es = new EventSource(url);
    this.metricsStream = es;

    es.addEventListener('open', () => {
      this.sseLive.set(true);
      this.sseConnected.set(true);
      this.sseRetryCount = 0;
    });

    es.addEventListener('message', (evt) => {
      if (!evt.data || evt.data === 'heartbeat') return;
      try {
        const payload = JSON.parse(evt.data) as Partial<DoraMetrics>;
        const current = this.metrics();
        if (current) {
          this.metrics.set({ ...current, ...payload });
        }
      } catch {
        // ignora payload malformado
      }
    });

    es.addEventListener('heartbeat', () => {
      // Sinal de vida — ignora silenciosamente
    });

    es.addEventListener('error', () => {
      this.sseLive.set(false);
      es.close();

      if (this.sseRetryCount < this.SSE_MAX_RETRIES) {
        const delay = Math.pow(2, this.sseRetryCount) * 1000; // 1s, 2s, 4s, 8s, 16s
        this.sseRetryCount++;
        setTimeout(() => this.connectSSE(projectId), delay);
      } else {
        this.sseConnected.set(false);
      }
    });
  }

  private closeSSE(): void {
    if (this.metricsStream) {
      this.metricsStream.close();
      this.metricsStream = undefined;
    }
    this.sseLive.set(false);
    this.sseConnected.set(false);
    this.sseRetryCount = 0;
  }

  // ---- AI narrative ----

  private loadNarrative(projectId: string): void {
    this.loadingNarrative.set(true);
    this.narrativeText.set(null);
    this.api.getProjectPredict(projectId, 30).pipe(
      catchError(() => of<PredictResponse>({ projectId, lookbackDays: 30, explanation: null, computedAt: '' })),
      finalize(() => this.loadingNarrative.set(false)),
    ).subscribe((r) => this.narrativeText.set(r.explanation));
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
    // Fecha SSE antes de recarregar (será reaberto após o load)
    this.closeSSE();

    // Em modo demo, usa os dados sintéticos do DemoService
    const metricsObs = this.demo.isDemo()
      ? this.demo.getMetrics(projectId).pipe(catchError(() => of(null)))
      : this.api.getProjectMetrics(projectId, this.selectedWindow).pipe(catchError(() => of(null)));

    const timeseriesObs = this.demo.isDemo()
      ? this.demo.getTimeseries(projectId).pipe(catchError(() => of({ points: [] as TimeseriesPoint[] })))
      : this.api.getProjectTimeseries(projectId, this.selectedWindow).pipe(catchError(() => of({ points: [] as TimeseriesPoint[] })));

    const achievementsObs = this.demo.isDemo()
      ? this.demo.getAchievements(projectId).pipe(catchError(() => of<ProjectAchievements | null>(null)))
      : this.api.getProjectAchievements(projectId, this.selectedWindow).pipe(catchError(() => of<ProjectAchievements | null>(null)));

    forkJoin({
      metrics: metricsObs,
      timeseries: timeseriesObs,
      deployments: this.api
        .listProjectDeployments(projectId, this.selectedWindow)
        .pipe(catchError(() => of([] as Deployment[]))),
      achievements: achievementsObs,
    })
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe(({ metrics, timeseries, deployments, achievements }) => {
        this.metrics.set(metrics);
        this.points.set(timeseries.points ?? []);
        this.deployments.set(deployments);
        this.achievements.set(achievements);
        // Abre SSE apenas em modo real (não demo) e scope=project
        if (!this.demo.isDemo()) {
          this.openSSE(projectId);
          this.loadNarrative(projectId);
        }
      });
  }

  private reloadTeam(teamId: string): void {
    this.loading.set(true);
    // SSE e narrativa não suportam scope=team ainda
    this.closeSSE();
    this.narrativeText.set(null);

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

  exportUrl(
    kind: 'deployments' | 'incidents' | 'merge_requests',
    format: 'csv' | 'json',
  ): string {
    return this.api.projectExportUrl(
      this.selectedProjectId!,
      kind,
      format,
      this.selectedWindow,
    );
  }

  private errorMessage(err: unknown): string {
    if (err instanceof Error) return err.message;
    return 'Erro ao carregar métricas';
  }
}
