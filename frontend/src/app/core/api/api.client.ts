import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import {
  CreatePersonRequest,
  Deployment,
  DoraMetrics,
  Identity,
  MergeSuggestion,
  Person,
  PersonWithIdentities,
  Project,
  TimeseriesResponse,
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

  healthz(): Observable<{ status: string }> {
    return this.http.get<{ status: string }>('/healthz');
  }
}
