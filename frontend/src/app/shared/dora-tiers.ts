/**
 * Espelha o backend `internal/calculator/classification.go` (DORA Report
 * 2023/2024). Mantenha em sincronia. Esses thresholds são derivados
 * client-side porque o endpoint `/projects/{id}/metrics` ainda não devolve
 * os limiares na resposta — quando devolver, prefira o payload do backend.
 */

import { Classification } from '../core/api/api.types';

const HOUR = 3600;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;
const MONTH = 30 * DAY;

export interface DoraThresholds {
  // Deployment Frequency (maior é melhor) — deploys/dia.
  dfElite: number;
  dfHigh: number;
  dfMedium: number;
  // Lead Time (menor é melhor) — segundos.
  ltElite: number;
  ltHigh: number;
  ltMedium: number;
  // Change Failure Rate (menor é melhor) — 0..1.
  cfrElite: number;
  cfrHigh: number;
  cfrMedium: number;
  // MTTR (menor é melhor) — segundos.
  mttrElite: number;
  mttrHigh: number;
  mttrMedium: number;
}

export const DEFAULT_THRESHOLDS: DoraThresholds = {
  dfElite: 1.0,
  dfHigh: 1 / 7,
  dfMedium: 1 / 30,

  ltElite: HOUR,
  ltHigh: WEEK,
  ltMedium: MONTH,

  cfrElite: 0.05,
  cfrHigh: 0.10,
  cfrMedium: 0.20,

  mttrElite: HOUR,
  mttrHigh: DAY,
  mttrMedium: WEEK,
};

export type MetricKey = 'df' | 'lt' | 'cfr' | 'mttr';

export const TIER_RANK: Record<Classification, number> = {
  elite: 4,
  high: 3,
  medium: 2,
  low: 1,
  insufficient_data: 0,
};

const REAL_TIERS: Classification[] = ['low', 'medium', 'high', 'elite'];

/**
 * Classifica um valor pra uma métrica (replica o backend).
 * Retorna `insufficient_data` se value for null/undefined ou (no caso DF)
 * <= 0.
 */
export function classifyMetric(
  metric: MetricKey,
  value: number | null | undefined,
  t: DoraThresholds = DEFAULT_THRESHOLDS,
): Classification {
  if (value === null || value === undefined) return 'insufficient_data';

  switch (metric) {
    case 'df':
      if (value <= 0) return 'insufficient_data';
      if (value >= t.dfElite) return 'elite';
      if (value >= t.dfHigh) return 'high';
      if (value >= t.dfMedium) return 'medium';
      return 'low';
    case 'lt':
      if (value < t.ltElite) return 'elite';
      if (value < t.ltHigh) return 'high';
      if (value < t.ltMedium) return 'medium';
      return 'low';
    case 'cfr':
      if (value <= t.cfrElite) return 'elite';
      if (value <= t.cfrHigh) return 'high';
      if (value <= t.cfrMedium) return 'medium';
      return 'low';
    case 'mttr':
      if (value < t.mttrElite) return 'elite';
      if (value < t.mttrHigh) return 'high';
      if (value < t.mttrMedium) return 'medium';
      return 'low';
  }
}

/**
 * Pior tier (menor rank) entre os fornecidos. `insufficient_data` é
 * ignorado, exceto se TODOS forem insufficient. Replica `WorstOf`.
 */
export function worstTier(tiers: Classification[]): Classification {
  let worst: Classification = 'insufficient_data';
  let worstRank = -1;
  for (const t of tiers) {
    const r = TIER_RANK[t];
    if (r === 0) continue;
    if (worstRank === -1 || r < worstRank) {
      worstRank = r;
      worst = t;
    }
  }
  return worst;
}

export interface NextTierProgress {
  /** Tier atual da métrica. */
  current: Classification;
  /** Próximo tier alcançável (`null` se já é elite ou insufficient). */
  next: Classification | null;
  /** Valor faltante absoluto pra chegar no próximo tier (na unidade da métrica). */
  delta: number | null;
  /** Progresso 0..1 dentro da faixa do tier atual. */
  progress: number;
  /** Texto humano da unidade ("deploys/dia", "h", "%"). */
  unit: string;
  /** Texto pronto para UI ("+0.34 deploys/dia para Elite"). */
  label: string;
}

/**
 * Calcula quanto falta para a métrica subir de tier. Se já é Elite,
 * retorna `next=null` e label "Você está no topo".
 * Se faltar dado, retorna progress=0 / next=null / label="—".
 *
 * Para métricas onde menor-é-melhor (LT/CFR/MTTR), `delta` é o quanto a
 * métrica precisa REDUZIR (sempre positivo na UI). Para DF, quanto precisa
 * AUMENTAR.
 */
export function nextTierProgress(
  metric: MetricKey,
  value: number | null | undefined,
  t: DoraThresholds = DEFAULT_THRESHOLDS,
): NextTierProgress {
  const current = classifyMetric(metric, value, t);
  if (current === 'insufficient_data' || value === null || value === undefined) {
    return {
      current,
      next: null,
      delta: null,
      progress: 0,
      unit: unitOf(metric),
      label: 'Sem dados suficientes',
    };
  }

  if (current === 'elite') {
    return {
      current,
      next: null,
      delta: 0,
      progress: 1,
      unit: unitOf(metric),
      label: 'Você está no topo',
    };
  }

  // Para cada métrica, descobre o threshold do próximo tier e do tier atual
  // (limite inferior da faixa atual) pra calcular o progresso fracionário.
  const { lower, upper, next } = boundsFor(metric, current, t);
  const unit = unitOf(metric);

  let delta: number;
  let progress: number;

  if (metric === 'df') {
    // maior é melhor → upper é o threshold do próximo tier.
    delta = Math.max(0, upper - value);
    const span = upper - lower;
    progress = span > 0 ? clamp01((value - lower) / span) : 0;
  } else {
    // menor é melhor → upper é o threshold do próximo tier (valor menor).
    delta = Math.max(0, value - upper);
    const span = lower - upper;
    progress = span > 0 ? clamp01((lower - value) / span) : 0;
  }

  return {
    current,
    next,
    delta,
    progress,
    unit,
    label: `+${formatDelta(metric, delta)} para ${tierName(next)}`,
  };
}

function clamp01(x: number): number {
  if (!isFinite(x)) return 0;
  return Math.min(1, Math.max(0, x));
}

function unitOf(metric: MetricKey): string {
  switch (metric) {
    case 'df': return 'deploys/dia';
    case 'lt': return 's';
    case 'cfr': return '%';
    case 'mttr': return 's';
  }
}

function tierName(t: Classification | null): string {
  if (!t) return '';
  switch (t) {
    case 'elite': return 'Elite';
    case 'high': return 'High';
    case 'medium': return 'Medium';
    case 'low': return 'Low';
    case 'insufficient_data': return 'sem dados';
  }
}

/**
 * Devolve o threshold do tier atual (lower) e do próximo (upper) +
 * a Classification do próximo tier.
 *
 * Para df: lower = limiar do tier atual; upper = limiar do próximo
 * (mais alto).
 * Para lt/cfr/mttr: lower = limiar do tier atual (valor MAIOR);
 * upper = limiar do próximo tier (valor MENOR).
 */
function boundsFor(
  metric: MetricKey,
  current: Classification,
  t: DoraThresholds,
): { lower: number; upper: number; next: Classification } {
  const idx = REAL_TIERS.indexOf(current);
  const next = REAL_TIERS[idx + 1] ?? 'elite';

  if (metric === 'df') {
    const thresholds = [0, t.dfMedium, t.dfHigh, t.dfElite, Number.POSITIVE_INFINITY];
    // current=low → idx 0; lower=thresholds[0]=0; upper=thresholds[1]=dfMedium
    // current=medium → lower=dfMedium; upper=dfHigh
    return { lower: thresholds[idx], upper: thresholds[idx + 1], next };
  }

  if (metric === 'lt') {
    // valores: elite < ltElite < high < ltHigh < medium < ltMedium < low
    // low: lower=∞ (cap visualmente) ; upper=ltMedium
    // medium: lower=ltMedium ; upper=ltHigh
    // high: lower=ltHigh ; upper=ltElite
    const ladder = [Number.POSITIVE_INFINITY, t.ltMedium, t.ltHigh, t.ltElite, 0];
    return { lower: ladder[idx], upper: ladder[idx + 1], next };
  }

  if (metric === 'cfr') {
    const ladder = [1.0, t.cfrMedium, t.cfrHigh, t.cfrElite, 0];
    return { lower: ladder[idx], upper: ladder[idx + 1], next };
  }

  // mttr
  const ladder = [Number.POSITIVE_INFINITY, t.mttrMedium, t.mttrHigh, t.mttrElite, 0];
  return { lower: ladder[idx], upper: ladder[idx + 1], next };
}

export function formatDelta(metric: MetricKey, delta: number): string {
  if (delta === 0) return '0';
  switch (metric) {
    case 'df':
      return `${delta.toFixed(2)} deploys/dia`;
    case 'cfr':
      return `${(delta * 100).toFixed(1)} pp`;
    case 'lt':
    case 'mttr':
      return formatDuration(delta);
  }
}

function formatDuration(seconds: number): string {
  if (!isFinite(seconds) || seconds <= 0) return '0s';
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${(seconds / 60).toFixed(0)}min`;
  if (seconds < 86400) return `${(seconds / 3600).toFixed(1)}h`;
  return `${(seconds / 86400).toFixed(1)}d`;
}

/**
 * Devolve cutoff "ideal" do tier (numérico) para cada métrica, em ordem
 * crescente de rank. Usado no painel "Por que esse tier?".
 */
export function cutoffsFor(metric: MetricKey, t: DoraThresholds = DEFAULT_THRESHOLDS): {
  low: string;
  medium: string;
  high: string;
  elite: string;
} {
  switch (metric) {
    case 'df':
      return {
        low: `< ${t.dfMedium.toFixed(3)} /dia`,
        medium: `≥ ${t.dfMedium.toFixed(3)} /dia`,
        high: `≥ ${t.dfHigh.toFixed(3)} /dia`,
        elite: `≥ ${t.dfElite.toFixed(2)} /dia`,
      };
    case 'lt':
      return {
        low: `≥ ${formatDuration(t.ltMedium)}`,
        medium: `< ${formatDuration(t.ltMedium)}`,
        high: `< ${formatDuration(t.ltHigh)}`,
        elite: `< ${formatDuration(t.ltElite)}`,
      };
    case 'cfr':
      return {
        low: `> ${(t.cfrMedium * 100).toFixed(0)}%`,
        medium: `≤ ${(t.cfrMedium * 100).toFixed(0)}%`,
        high: `≤ ${(t.cfrHigh * 100).toFixed(0)}%`,
        elite: `≤ ${(t.cfrElite * 100).toFixed(0)}%`,
      };
    case 'mttr':
      return {
        low: `≥ ${formatDuration(t.mttrMedium)}`,
        medium: `< ${formatDuration(t.mttrMedium)}`,
        high: `< ${formatDuration(t.mttrHigh)}`,
        elite: `< ${formatDuration(t.mttrElite)}`,
      };
  }
}

export function formatMetricValue(metric: MetricKey, value: number | null | undefined): string {
  if (value === null || value === undefined) return '—';
  switch (metric) {
    case 'df':
      return `${value.toFixed(2)}/dia`;
    case 'cfr':
      return `${(value * 100).toFixed(1)}%`;
    case 'lt':
    case 'mttr':
      return formatDuration(value);
  }
}
