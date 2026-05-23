import { Injectable, signal } from '@angular/core';
import { Observable, of } from 'rxjs';
import { delay } from 'rxjs/operators';

import {
  DoraMetrics,
  ProjectAchievements,
  TimeseriesResponse,
} from '../api/api.types';

// ---- Dataset sintético inline ----
// 3 projetos demo com 90 dias de história simulada.
// Projeto A: começa Elite, cai para High, está recovering.
// Projeto B: estável em Medium.
// Projeto C: recém subiu para High.

export interface DemoProject {
  id: string;
  name: string;
  pathWithNamespace: string;
  slug: string;
  teamId: null;
  active: true;
}

const DEMO_PROJECTS: DemoProject[] = [
  {
    id: 'demo-alpha',
    name: 'Alpha Platform',
    pathWithNamespace: 'demo/alpha-platform',
    slug: 'alpha-platform',
    teamId: null,
    active: true,
  },
  {
    id: 'demo-beta',
    name: 'Beta Service',
    pathWithNamespace: 'demo/beta-service',
    slug: 'beta-service',
    teamId: null,
    active: true,
  },
  {
    id: 'demo-gamma',
    name: 'Gamma API',
    pathWithNamespace: 'demo/gamma-api',
    slug: 'gamma-api',
    teamId: null,
    active: true,
  },
];

// Gera pontos de série temporal para N dias
function generatePoints(
  days: number,
  baseCount: number,
  volatility = 0.3,
): { day: string; deployCount: number }[] {
  const points: { day: string; deployCount: number }[] = [];
  const now = new Date();
  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(now);
    d.setDate(d.getDate() - i);
    const jitter = 1 + (Math.random() - 0.5) * volatility * 2;
    const count = Math.max(0, Math.round(baseCount * jitter));
    points.push({ day: d.toISOString().substring(0, 10), deployCount: count });
  }
  return points;
}

const DEMO_METRICS: Record<string, DoraMetrics> = {
  'demo-alpha': {
    projectId: 'demo-alpha',
    windowDays: 30,
    computedAt: new Date().toISOString(),
    deploymentFrequency: 1.8,         // ~every 13h → High (recovering from Elite)
    leadTimeMedianSeconds: 23 * 3600, // 23h → High
    changeFailureRate: 0.07,          // 7% → High
    mttrMeanSeconds: 2.5 * 3600,     // 2.5h → Elite
    classification: 'high',
    sampleSize: 54,
  },
  'demo-beta': {
    projectId: 'demo-beta',
    windowDays: 30,
    computedAt: new Date().toISOString(),
    deploymentFrequency: 0.3,         // ~every 3 days → Medium
    leadTimeMedianSeconds: 5 * 86400, // 5 days → Medium
    changeFailureRate: 0.18,          // 18% → Low
    mttrMeanSeconds: 20 * 3600,      // 20h → Medium
    classification: 'medium',
    sampleSize: 9,
  },
  'demo-gamma': {
    projectId: 'demo-gamma',
    windowDays: 30,
    computedAt: new Date().toISOString(),
    deploymentFrequency: 0.8,         // ~every 30h → High
    leadTimeMedianSeconds: 18 * 3600, // 18h → High
    changeFailureRate: 0.12,          // 12% → High
    mttrMeanSeconds: 6 * 3600,       // 6h → High
    classification: 'high',
    sampleSize: 24,
  },
};

const DEMO_TIMESERIES: Record<string, TimeseriesResponse> = {
  'demo-alpha': {
    projectId: 'demo-alpha',
    windowDays: 30,
    metric: 'deployment_frequency',
    points: generatePoints(30, 2, 0.4),
  },
  'demo-beta': {
    projectId: 'demo-beta',
    windowDays: 30,
    metric: 'deployment_frequency',
    points: generatePoints(30, 0.3, 0.6),
  },
  'demo-gamma': {
    projectId: 'demo-gamma',
    windowDays: 30,
    metric: 'deployment_frequency',
    points: generatePoints(30, 1, 0.35),
  },
};

const DEMO_ACHIEVEMENTS: Record<string, ProjectAchievements> = {
  'demo-alpha': {
    projectId: 'demo-alpha',
    windowDays: 30,
    daysSinceLastIncident: 14,
    currentClassification: 'high',
    achievements: [
      {
        code: 'streak_7',
        title: '7 dias sem incident',
        description: 'Nenhum incident de produção nos últimos 7 dias.',
        icon: 'local_fire_department',
        unlockedAt: new Date(Date.now() - 7 * 86400 * 1000).toISOString().substring(0, 10),
      },
      {
        code: 'high_freq',
        title: 'Deploys frequentes',
        description: 'Mais de 1 deploy/dia na janela — práticas CI/CD avançadas.',
        icon: 'rocket_launch',
        unlockedAt: new Date(Date.now() - 3 * 86400 * 1000).toISOString().substring(0, 10),
      },
    ],
  },
  'demo-beta': {
    projectId: 'demo-beta',
    windowDays: 30,
    daysSinceLastIncident: 2,
    currentClassification: 'medium',
    achievements: [],
  },
  'demo-gamma': {
    projectId: 'demo-gamma',
    windowDays: 30,
    daysSinceLastIncident: 21,
    currentClassification: 'high',
    achievements: [
      {
        code: 'streak_21',
        title: '21 dias sem incident',
        description: 'Excelente estabilidade — mais de 3 semanas sem incident.',
        icon: 'emoji_events',
        unlockedAt: new Date(Date.now() - 1 * 86400 * 1000).toISOString().substring(0, 10),
      },
    ],
  },
};

@Injectable({ providedIn: 'root' })
export class DemoService {
  /** Sinal reativo: true quando modo demo está ativo */
  readonly isDemo = signal(false);

  /**
   * Detecta `?demo=true` na URL e ativa o modo demo.
   * Chamar no bootstrap da aplicação (AppComponent.constructor).
   */
  init(): void {
    const params = new URLSearchParams(window.location.search);
    if (params.get('demo') === 'true') {
      this.isDemo.set(true);
    }
  }

  getProjects(): Observable<DemoProject[]> {
    return of(DEMO_PROJECTS).pipe(delay(200));
  }

  getMetrics(projectId: string): Observable<DoraMetrics> {
    const m = DEMO_METRICS[projectId] ?? DEMO_METRICS['demo-alpha'];
    return of(m).pipe(delay(150));
  }

  getTimeseries(projectId: string): Observable<TimeseriesResponse> {
    const t = DEMO_TIMESERIES[projectId] ?? DEMO_TIMESERIES['demo-alpha'];
    return of(t).pipe(delay(180));
  }

  getAchievements(projectId: string): Observable<ProjectAchievements> {
    const a = DEMO_ACHIEVEMENTS[projectId] ?? DEMO_ACHIEVEMENTS['demo-alpha'];
    return of(a).pipe(delay(120));
  }
}
