import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import {
  Deployment,
  DoraMetrics,
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

  healthz(): Observable<{ status: string }> {
    return this.http.get<{ status: string }>('/healthz');
  }
}
