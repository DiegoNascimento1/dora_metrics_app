/* Tipos manuais para o MVP. Em CI rodamos `make fe-gen-types` que substitui
 * isto pelo arquivo gerado em `generated/api-types.ts` a partir de openapi.yaml. */

export type Classification = 'elite' | 'high' | 'medium' | 'low' | 'insufficient_data';

export interface Project {
  id: string;
  slug: string;
  name: string;
  pathWithNamespace: string;
  teamId: string | null;
  active: boolean;
}

export interface DoraMetrics {
  projectId: string;
  windowDays: 7 | 30 | 90;
  computedAt: string;
  deploymentFrequency: number;
  leadTimeMedianSeconds: number | null;
  changeFailureRate: number | null;
  mttrMeanSeconds: number | null;
  classification: Classification;
  sampleSize: number;
}

export interface TimeseriesPoint {
  day: string;          // YYYY-MM-DD
  deployCount: number;
}

export interface TimeseriesResponse {
  projectId: string;
  windowDays: number;
  metric: string;
  points: TimeseriesPoint[];
}

export interface Deployment {
  id: string;
  sha: string;
  ref: string | null;
  status: string;
  triggeredBy: string | null;
  startedAt: string | null;
  finishedAt: string | null;
  environmentName: string;
}
