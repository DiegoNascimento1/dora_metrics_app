import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import {
  AlertEvent,
  AlertRule,
  CreateAlertRuleRequest,
  CreatePersonRequest,
  CreateSourceInstanceRequest,
  CreateTeamRequest,
  Deployment,
  DoraMetrics,
  Identity,
  MergeSuggestion,
  Person,
  PersonMetrics,
  PersonWithIdentities,
  Project,
  ProjectAchievements,
  SourceInstance,
  Team,
  TestConnectionRequest,
  TestConnectionResponse,
  TimeseriesResponse,
  UpdateAlertRuleRequest,
  UpdateTeamRequest,
  WeeklyDigest,
} from './api.types';

const API_BASE = '/api/v1';

@Injectable({ providedIn: 'root' })
export class ApiClient {
  private http = inject(HttpClient);

  listProjects(): Observable<Project[]> {
    return this.http.get<Project[]>(`${API_BASE}/projects`);
  }

  getProjectMetrics(
    projectId: string,
    window: '7d' | '30d' | '90d' = '30d',
  ): Observable<DoraMetrics> {
    return this.http.get<DoraMetrics>(
      `${API_BASE}/projects/${projectId}/metrics`,
      { params: { window } },
    );
  }

  getProjectTimeseries(
    projectId: string,
    window: '7d' | '30d' | '90d' = '30d',
  ): Observable<TimeseriesResponse> {
    return this.http.get<TimeseriesResponse>(
      `${API_BASE}/projects/${projectId}/timeseries`,
      { params: { window } },
    );
  }

  listProjectDeployments(
    projectId: string,
    window: '7d' | '30d' | '90d' = '30d',
  ): Observable<Deployment[]> {
    return this.http.get<Deployment[]>(
      `${API_BASE}/projects/${projectId}/deployments`,
      { params: { window } },
    );
  }

  getProjectAchievements(
    projectId: string,
    window: '7d' | '30d' | '90d' = '30d',
  ): Observable<ProjectAchievements> {
    return this.http.get<ProjectAchievements>(
      `${API_BASE}/projects/${projectId}/achievements`,
      { params: { window } },
    );
  }

  // ---- people / identities (Fase 3.5) ----

  listPeople(tenant: string): Observable<PersonWithIdentities[]> {
    return this.http.get<PersonWithIdentities[]>(`${API_BASE}/people`, {
      params: { tenant },
    });
  }

  createPerson(body: CreatePersonRequest): Observable<Person> {
    return this.http.post<Person>(`${API_BASE}/people`, body);
  }

  listUnlinkedIdentities(tenant: string): Observable<Identity[]> {
    return this.http.get<Identity[]>(`${API_BASE}/identities/unlinked`, {
      params: { tenant },
    });
  }

  getAutomatchSuggestions(tenant: string): Observable<MergeSuggestion[]> {
    return this.http.get<MergeSuggestion[]>(`${API_BASE}/identities/automatch`, {
      params: { tenant },
    });
  }

  linkIdentity(
    identityId: string,
    body: { personId: string; linkedBy?: string },
  ): Observable<Identity> {
    return this.http.post<Identity>(
      `${API_BASE}/identities/${identityId}/link`,
      body,
    );
  }

  getPersonMetrics(
    personId: string,
    window: '7d' | '30d' | '90d' = '30d',
  ): Observable<PersonMetrics> {
    return this.http.get<PersonMetrics>(
      `${API_BASE}/people/${personId}/metrics`,
      { params: { window } },
    );
  }

  // ---- source instances (settings) ----

  listSourceInstances(tenant: string): Observable<SourceInstance[]> {
    return this.http.get<SourceInstance[]>(`${API_BASE}/source-instances`, {
      params: { tenant },
    });
  }

  createSourceInstance(body: CreateSourceInstanceRequest): Observable<SourceInstance> {
    return this.http.post<SourceInstance>(`${API_BASE}/source-instances`, body);
  }

  deleteSourceInstance(id: string): Observable<void> {
    return this.http.delete<void>(`${API_BASE}/source-instances/${id}`);
  }

  testConnection(body: TestConnectionRequest): Observable<TestConnectionResponse> {
    return this.http.post<TestConnectionResponse>(
      `${API_BASE}/source-instances/test`,
      body,
    );
  }

  // ---- teams (multi-team UI) ----

  listTeams(tenant: string): Observable<Team[]> {
    return this.http.get<Team[]>(`${API_BASE}/teams`, { params: { tenant } });
  }

  createTeam(body: CreateTeamRequest): Observable<Team> {
    return this.http.post<Team>(`${API_BASE}/teams`, body);
  }

  updateTeam(id: string, body: UpdateTeamRequest): Observable<Team> {
    return this.http.patch<Team>(`${API_BASE}/teams/${id}`, body);
  }

  deleteTeam(id: string): Observable<void> {
    return this.http.delete<void>(`${API_BASE}/teams/${id}`);
  }

  assignProjectToTeam(teamId: string, projectId: string): Observable<Project> {
    return this.http.post<Project>(
      `${API_BASE}/teams/${teamId}/projects`,
      { projectId },
    );
  }

  unassignProjectFromTeam(projectId: string): Observable<Project> {
    return this.http.post<Project>(
      `${API_BASE}/projects/${projectId}/unassign-team`,
      {},
    );
  }

  getTeamMetrics(
    teamId: string,
    window: '7d' | '30d' | '90d' = '30d',
  ): Observable<DoraMetrics> {
    return this.http.get<DoraMetrics>(
      `${API_BASE}/teams/${teamId}/metrics`,
      { params: { window } },
    );
  }

  getTeamTimeseries(
    teamId: string,
    window: '7d' | '30d' | '90d' = '30d',
  ): Observable<TimeseriesResponse> {
    return this.http.get<TimeseriesResponse>(
      `${API_BASE}/teams/${teamId}/timeseries`,
      { params: { window } },
    );
  }

  // ---- weekly digest (Fase 4) ----

  getProjectDigest(
    projectId: string,
    week?: string,
  ): Observable<WeeklyDigest> {
    const params: Record<string, string> = {};
    if (week) params['week'] = week;
    return this.http.get<WeeklyDigest>(
      `${API_BASE}/projects/${projectId}/digest`,
      { params },
    );
  }

  getTeamDigest(teamId: string, week?: string): Observable<WeeklyDigest> {
    const params: Record<string, string> = {};
    if (week) params['week'] = week;
    return this.http.get<WeeklyDigest>(
      `${API_BASE}/teams/${teamId}/digest`,
      { params },
    );
  }

  // ---- alert rules (Fase 4) ----

  listAlertRules(tenant: string): Observable<AlertRule[]> {
    return this.http.get<AlertRule[]>(`${API_BASE}/alert-rules`, {
      params: { tenant },
    });
  }

  createAlertRule(body: CreateAlertRuleRequest): Observable<AlertRule> {
    return this.http.post<AlertRule>(`${API_BASE}/alert-rules`, body);
  }

  updateAlertRule(id: string, body: UpdateAlertRuleRequest): Observable<AlertRule> {
    return this.http.patch<AlertRule>(`${API_BASE}/alert-rules/${id}`, body);
  }

  deleteAlertRule(id: string): Observable<void> {
    return this.http.delete<void>(`${API_BASE}/alert-rules/${id}`);
  }

  listAlertEvents(tenant: string, limit = 50): Observable<AlertEvent[]> {
    return this.http.get<AlertEvent[]>(`${API_BASE}/alert-events`, {
      params: { tenant, limit },
    });
  }

  healthz(): Observable<{ status: string }> {
    return this.http.get<{ status: string }>('/healthz');
  }

  // ---- export (Fase 4 — Critério de saída) ----

  /**
   * Constrói a URL do dump bruto da janela. O backend serve com
   * Content-Disposition attachment, então um <a download> ou
   * window.location dispara o salvamento direto pelo navegador.
   */
  projectExportUrl(
    projectId: string,
    kind: 'deployments' | 'incidents' | 'merge_requests',
    format: 'csv' | 'json' = 'csv',
    window: '7d' | '30d' | '90d' = '30d',
  ): string {
    return `${API_BASE}/projects/${projectId}/export?kind=${kind}&format=${format}&window=${window}`;
  }
}
