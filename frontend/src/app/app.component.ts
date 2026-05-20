import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { MatToolbarModule } from '@angular/material/toolbar';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatTooltipModule } from '@angular/material/tooltip';

import { ThemeService } from './core/theme/theme.service';

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
  ],
  template: `
    <mat-toolbar class="app-bar">
      <span class="brand">
        <mat-icon class="brand-icon">trending_up</mat-icon>
        <span class="brand-text">DORA Metrics</span>
      </span>
      <span class="spacer"></span>
      <a mat-button routerLink="/dashboard" routerLinkActive="active">Dashboard</a>
      <a mat-button routerLink="/projects" routerLinkActive="active">Projetos</a>
      <a mat-button routerLink="/people" routerLinkActive="active">Pessoas</a>

      <button
        mat-icon-button
        class="theme-toggle"
        (click)="theme.toggle()"
        [matTooltip]="theme.isDark() ? 'Mudar para tema claro' : 'Mudar para tema escuro'"
        [attr.aria-label]="
          theme.isDark() ? 'Mudar para tema claro' : 'Mudar para tema escuro'
        "
      >
        <mat-icon>{{ theme.isDark() ? 'light_mode' : 'dark_mode' }}</mat-icon>
      </button>
    </mat-toolbar>

    <main class="container">
      <router-outlet />
    </main>
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
        gap: var(--space-2);
      }
      .brand-icon {
        color: var(--color-brand);
      }
      .brand-text {
        font-weight: 700;
        letter-spacing: -0.01em;
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
    `,
  ],
})
export class AppComponent {
  protected theme = inject(ThemeService);
}
