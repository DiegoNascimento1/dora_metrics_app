import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import {
  MAT_DIALOG_DATA,
  MatDialogActions,
  MatDialogClose,
  MatDialogContent,
  MatDialogRef,
  MatDialogTitle,
} from '@angular/material/dialog';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';

import { Classification, DoraMetrics } from '../core/api/api.types';
import {
  DEFAULT_THRESHOLDS,
  MetricKey,
  TIER_RANK,
  classifyMetric,
  cutoffsFor,
  formatMetricValue,
  worstTier,
} from './dora-tiers';

export interface TierExplainData {
  metrics: DoraMetrics;
  /** Tier combinado calculado pelo backend — destacamos a métrica que rebaixou. */
  combined: Classification;
  /** Rótulo do escopo: "Projeto: foo" ou "Time: bar". */
  scopeLabel: string;
}

interface MetricRow {
  key: MetricKey;
  label: string;
  value: string;
  tier: Classification;
  cutoffCurrent: string;
  cutoffNext: string;
  isDragging: boolean;
}

/**
 * Dialog "Por que esse tier?" — mostra os 4 valores DORA, o tier que cada
 * um teria isoladamente, e marca explicitamente a métrica que está
 * rebaixando o tier combinado.
 *
 * O tier combinado é o pior entre as 4 (replica `WorstOf` do backend).
 * Métricas com `insufficient_data` são ignoradas na combinação.
 */
@Component({
  selector: 'app-tier-explain-dialog',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    MatDialogTitle,
    MatDialogContent,
    MatDialogActions,
    MatDialogClose,
    MatButtonModule,
    MatIconModule,
  ],
  template: `
    <h2 mat-dialog-title>
      <mat-icon class="head-icon" fontIcon="help_center" aria-hidden="true"></mat-icon>
      Por que esse tier?
    </h2>
    <mat-dialog-content>
      <p class="scope">
        <span class="muted">Escopo:</span> {{ data.scopeLabel }} ·
        <span class="muted">tier combinado:</span>
        <span [class]="'badge tier-' + data.combined">{{ data.combined }}</span>
      </p>
      <p class="hint">
        O tier combinado é o <strong>pior</strong> entre as 4 métricas
        (replica a regra do backend). A métrica destacada em laranja é
        a que está puxando o resultado para baixo.
      </p>

      <ul class="rows" aria-label="Métricas DORA">
        @for (r of rows(); track r.key) {
          <li class="row" [class.row-dragging]="r.isDragging">
            <div class="row-head">
              <strong>{{ r.label }}</strong>
              <span [class]="'badge tier-' + r.tier">{{ r.tier }}</span>
            </div>
            <div class="row-body">
              <span class="value">{{ r.value }}</span>
              <span class="muted">
                Atual: <code>{{ r.cutoffCurrent }}</code> · Próximo:
                <code>{{ r.cutoffNext }}</code>
              </span>
            </div>
            @if (r.isDragging) {
              <div class="dragging-note">
                <mat-icon fontIcon="arrow_downward" aria-hidden="true"></mat-icon>
                Rebaixando o tier combinado
              </div>
            }
          </li>
        }
      </ul>

      <p class="footnote muted">
        Limiares baseados no DORA Report 2023/2024. Documentação:
        <a
          href="https://github.com/diegonascimentoo/dora_metrics_app/blob/main/docs/01-dora-metrics.md"
          target="_blank"
          rel="noopener"
        >docs/01-dora-metrics.md</a>.
      </p>
    </mat-dialog-content>
    <mat-dialog-actions align="end">
      <button mat-button mat-dialog-close>Fechar</button>
    </mat-dialog-actions>
  `,
  styles: [
    `
      h2 {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
      }
      .head-icon {
        color: var(--color-brand);
      }
      .scope {
        margin: 0 0 var(--space-3);
      }
      .hint {
        background: var(--color-surface-subtle);
        border-left: 3px solid var(--color-brand);
        padding: var(--space-3);
        border-radius: var(--radius-sm);
        margin: 0 0 var(--space-4);
        font-size: var(--font-size-sm);
        line-height: 1.5;
      }
      .rows {
        list-style: none;
        padding: 0;
        margin: 0 0 var(--space-4);
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
      }
      .row {
        padding: var(--space-3);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-md);
        background: var(--color-surface);
        transition: border-color var(--transition-base), background var(--transition-base);
      }
      .row-dragging {
        border-color: var(--color-tier-low);
        background: var(--color-tier-low-bg);
      }
      .row-head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-2);
        margin-bottom: var(--space-1);
      }
      .row-body {
        display: flex;
        align-items: baseline;
        gap: var(--space-3);
        flex-wrap: wrap;
      }
      .value {
        font-size: var(--font-size-lg);
        font-weight: 600;
        color: var(--color-text-primary);
        font-variant-numeric: tabular-nums;
      }
      .dragging-note {
        margin-top: var(--space-2);
        display: inline-flex;
        align-items: center;
        gap: var(--space-1);
        color: var(--color-tier-low);
        font-weight: 600;
        font-size: var(--font-size-sm);
      }
      .dragging-note mat-icon {
        font-size: 16px;
        height: 16px;
        width: 16px;
      }
      .badge {
        display: inline-block;
        padding: 2px 10px;
        border-radius: 999px;
        font-size: var(--font-size-xs);
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.04em;
      }
      .tier-elite { background: var(--color-tier-elite-bg); color: var(--color-tier-elite); }
      .tier-high { background: var(--color-tier-high-bg); color: var(--color-tier-high); }
      .tier-medium { background: var(--color-tier-medium-bg); color: var(--color-tier-medium); }
      .tier-low { background: var(--color-tier-low-bg); color: var(--color-tier-low); }
      .tier-insufficient_data { background: var(--color-tier-na-bg); color: var(--color-tier-na); }
      .footnote {
        font-size: var(--font-size-xs);
      }
      code {
        font-family: var(--font-mono);
        font-size: 0.85em;
      }
    `,
  ],
})
export class TierExplainDialogComponent {
  protected dialogRef = inject(MatDialogRef<TierExplainDialogComponent>);
  protected data: TierExplainData = inject(MAT_DIALOG_DATA);

  protected rows = computed<MetricRow[]>(() => {
    const m = this.data.metrics;
    const t = DEFAULT_THRESHOLDS;

    const dfTier = classifyMetric('df', m.deploymentFrequency, t);
    const ltTier = classifyMetric('lt', m.leadTimeMedianSeconds, t);
    const cfrTier = classifyMetric('cfr', m.changeFailureRate, t);
    const mttrTier = classifyMetric('mttr', m.mttrMeanSeconds, t);

    // Tier combinado calculado igual ao backend → quem tem o menor rank
    // (entre os reais) é a métrica que rebaixou. Pode haver mais de uma
    // empatada — marcamos todas como "dragging".
    const computedCombined = worstTier([dfTier, ltTier, cfrTier, mttrTier]);
    const draggingRank = TIER_RANK[computedCombined];

    const isDragging = (t: Classification): boolean =>
      TIER_RANK[t] !== 0 && TIER_RANK[t] === draggingRank;

    return [
      {
        key: 'df',
        label: 'Deployment Frequency',
        value: formatMetricValue('df', m.deploymentFrequency),
        tier: dfTier,
        ...split(cutoffsFor('df', t), dfTier),
        isDragging: isDragging(dfTier),
      },
      {
        key: 'lt',
        label: 'Lead Time (mediana)',
        value: formatMetricValue('lt', m.leadTimeMedianSeconds),
        tier: ltTier,
        ...split(cutoffsFor('lt', t), ltTier),
        isDragging: isDragging(ltTier),
      },
      {
        key: 'cfr',
        label: 'Change Failure Rate',
        value: formatMetricValue('cfr', m.changeFailureRate),
        tier: cfrTier,
        ...split(cutoffsFor('cfr', t), cfrTier),
        isDragging: isDragging(cfrTier),
      },
      {
        key: 'mttr',
        label: 'MTTR (média)',
        value: formatMetricValue('mttr', m.mttrMeanSeconds),
        tier: mttrTier,
        ...split(cutoffsFor('mttr', t), mttrTier),
        isDragging: isDragging(mttrTier),
      },
    ];
  });
}

function split(
  c: { low: string; medium: string; high: string; elite: string },
  tier: Classification,
): { cutoffCurrent: string; cutoffNext: string } {
  const ladder: Classification[] = ['low', 'medium', 'high', 'elite'];
  const idx = ladder.indexOf(tier);
  if (idx < 0) {
    return { cutoffCurrent: '—', cutoffNext: '—' };
  }
  const current = (c as Record<Classification, string>)[tier] ?? '—';
  const next = idx === ladder.length - 1 ? 'topo' : (c as Record<Classification, string>)[ladder[idx + 1]];
  return { cutoffCurrent: current, cutoffNext: next };
}
