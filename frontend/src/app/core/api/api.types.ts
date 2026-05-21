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

export interface Identity {
  id: string;
  kind: 'gitlab' | 'jira';
  externalId: string | null;
  externalUsername: string;
  externalEmail: string | null;
  personId: string | null;
  linkedAt: string | null;
  linkedBy: string | null;
}

export interface Person {
  id: string;
  displayName: string;
  primaryEmail: string | null;
  avatarUrl: string | null;
  createdAt: string;
}

export interface PersonWithIdentities extends Person {
  identities: Identity[];
}

export interface MergeSuggestion {
  a: Identity;
  b: Identity;
  reason: 'email_exact' | 'username_exact';
  score: number;
}

export interface CreatePersonRequest {
  tenant: string;
  displayName: string;
  primaryEmail?: string;
  avatarUrl?: string;
  identityIds?: string[];
}

export interface PersonMetrics {
  personId: string;
  windowDays: number;
  deploymentsTriggered: number;
  leadTimeMedianSeconds: number | null;
  leadTimeSampleSize: number;
  incidentsLinked: number;
}

export interface Achievement {
  code: string;
  title: string;
  description: string;
  icon: string;        // Material Symbol Outlined name
  unlockedAt: string;  // ISO date
}

export interface ProjectAchievements {
  projectId: string;
  windowDays: number;
  daysSinceLastIncident: number;   // -1 = sem incidents registrados
  currentClassification: Classification;
  achievements: Achievement[];
}

export interface SourceInstance {
  id: string;
  kind: 'gitlab' | 'jira';
  baseUrl: string;
  displayName: string;
  authRef: string;
  authEmail?: string;
  hasSecret: boolean;
  createdAt: string;
}

export interface CreateSourceInstanceRequest {
  tenant: string;
  kind: 'gitlab' | 'jira';
  baseUrl: string;
  displayName: string;
  secret: string;
  authEmail?: string;
}

export interface TestConnectionRequest {
  kind: 'gitlab' | 'jira';
  baseUrl: string;
  secret: string;
  authEmail?: string;
}

export interface TestConnectionResponse {
  ok: boolean;
  status?: number;
  message?: string;
}

export interface Team {
  id: string;
  slug: string;
  name: string;
  color: string | null;
  emoji: string | null;
  createdAt: string;
}

export interface CreateTeamRequest {
  tenant: string;
  slug: string;
  name: string;
  color?: string;
  emoji?: string;
}

export interface UpdateTeamRequest {
  name?: string | null;
  color?: string | null;
  emoji?: string | null;
}
