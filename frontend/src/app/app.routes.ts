import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: '',
    pathMatch: 'full',
    redirectTo: 'dashboard',
  },
  {
    path: 'dashboard',
    loadComponent: () =>
      import('./features/dashboard/dashboard.component').then(
        (m) => m.DashboardComponent,
      ),
  },
  {
    path: 'projects',
    loadComponent: () =>
      import('./features/projects/projects.component').then(
        (m) => m.ProjectsComponent,
      ),
  },
  {
    path: 'people',
    loadComponent: () =>
      import('./features/people/people.component').then(
        (m) => m.PeopleComponent,
      ),
  },
  {
    path: 'teams',
    loadComponent: () =>
      import('./features/teams/teams.component').then(
        (m) => m.TeamsComponent,
      ),
  },
  {
    path: 'compare',
    loadComponent: () =>
      import('./features/compare/compare.component').then(
        (m) => m.CompareComponent,
      ),
  },
  {
    path: 'leaderboard',
    loadComponent: () =>
      import('./features/leaderboard/leaderboard.component').then(
        (m) => m.LeaderboardComponent,
      ),
  },
  {
    path: 'alerts',
    loadComponent: () =>
      import('./features/alerts/alerts.component').then(
        (m) => m.AlertsComponent,
      ),
  },
  {
    path: 'settings',
    loadComponent: () =>
      import('./features/settings/settings.component').then(
        (m) => m.SettingsComponent,
      ),
  },
  {
    // OIDC callback — a lib angular-auth-oidc-client lê os query params
    // (?code=...&state=...) e finaliza o login automaticamente. O
    // componente em si só redireciona pro dashboard depois.
    path: 'auth/callback',
    loadComponent: () =>
      import('./features/auth/callback.component').then(
        (m) => m.AuthCallbackComponent,
      ),
  },
  {
    // Wizard de onboarding self-service (Fase 9)
    path: 'setup',
    loadComponent: () =>
      import('./features/setup/setup-wizard.component').then(
        (m) => m.SetupWizardComponent,
      ),
  },
  {
    // Tela de anomalias multivariadas por projeto (Fase 7)
    path: 'projects/:id/anomalies',
    loadComponent: () =>
      import('./features/anomalies/anomalies.component').then(
        (m) => m.AnomaliesComponent,
      ),
  },
  {
    path: '**',
    redirectTo: 'dashboard',
  },
];
