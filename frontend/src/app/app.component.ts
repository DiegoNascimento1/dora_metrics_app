import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { MatToolbarModule } from '@angular/material/toolbar';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatTooltipModule } from '@angular/material/tooltip';

import { ThemeService } from './core/theme/theme.service';
import {
  OnboardingService,
  OnboardingTourComponent,
} from './shared/onboarding-tour.component';

@Component({
  selector: 'app-root',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    RouterOutlet,
    RouterLink,
    RouterLinkActive,
    MatToolbarModule,
    MatButtonModule,
    MatIconModule,
    MatTooltipModule,
    OnboardingTourComponent,
  ],
  template: `
    <mat-toolbar class="app-bar">
      <span class="brand">
        <img src="logo.svg" alt="" class="brand-logo" width="28" height="28" />
        <span class="brand-text">DORA Metrics</span>
      </span>
      <span class="spacer"></span>
      <a mat-button routerLink="/dashboard" routerLinkActive="active">Dashboard</a>
      <a mat-button routerLink="/compare" routerLinkActive="active">
        <mat-icon class="nav-icon" fontIcon="compare_arrows"></mat-icon>
        Comparar
      </a>
      <a mat-button routerLink="/leaderboard" routerLinkActive="active">
        <mat-icon class="nav-icon" fontIcon="leaderboard"></mat-icon>
        Leaderboard
      </a>
      <a mat-button routerLink="/projects" routerLinkActive="active">Projetos</a>
      <a mat-button routerLink="/teams" routerLinkActive="active">Times</a>
      <a mat-button routerLink="/people" routerLinkActive="active">Pessoas</a>
      <a mat-button routerLink="/alerts" routerLinkActive="active">
        <mat-icon class="nav-icon" fontIcon="notifications_active"></mat-icon>
        Alertas
      </a>
      <a mat-button routerLink="/settings" routerLinkActive="active">
        <mat-icon class="nav-icon" fontIcon="settings"></mat-icon>
        Configurações
      </a>

      <button
        mat-icon-button
        class="theme-toggle"
        (click)="theme.toggle()"
        [matTooltip]="theme.isDark() ? 'Mudar para tema claro' : 'Mudar para tema escuro'"
        [attr.aria-label]="
          theme.isDark() ? 'Mudar para tema claro' : 'Mudar para tema escuro'
        "
      >
        <mat-icon [fontIcon]="theme.isDark() ? 'light_mode' : 'dark_mode' "></mat-icon>
      </button>
    </mat-toolbar>

    <main class="container">
      <router-outlet />
    </main>

    <app-onboarding-tour />
  `,
  styles: [
    `
      .app-bar {
        background: var(--color-bg-elevated) !important;
        color: var(--color-text-primary) !important;
        border-bottom: 1px solid var(--color-border);
        box-shadow: var(--shadow-sm);
      }
      .brand {
        display: inline-flex;
        align-items: center;
        gap: 10px;
      }
      .brand-logo {
        display: block;
        flex-shrink: 0;
      }
      .brand-text {
        font-weight: 700;
        letter-spacing: -0.015em;
        font-size: 1.0625rem;
      }
      .spacer {
        flex: 1 1 auto;
      }
      a.active {
        color: var(--color-brand);
        font-weight: 600;
        position: relative;
      }
      a.active::after {
        content: '';
        position: absolute;
        bottom: 6px;
        left: 16px;
        right: 16px;
        height: 2px;
        background: var(--color-brand);
        border-radius: 1px;
      }
      .theme-toggle {
        margin-left: var(--space-2);
        color: var(--color-text-secondary);
      }
      .theme-toggle:hover {
        color: var(--color-brand);
      }
      .nav-icon {
        font-size: 18px;
        height: 18px;
        width: 18px;
        margin-right: 4px;
        vertical-align: middle;
      }
    `,
  ],
})
export class AppComponent {
  protected theme = inject(ThemeService);
  private tour = inject(OnboardingService);

  constructor() {
    // Inicia o tour na primeira visita (não força se o user já viu).
    // Aguarda 1 tick para o DOM do dashboard ter renderizado os spotlights.
    queueMicrotask(() => this.tour.start(false));
  }
}
