import {
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
} from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';

import { ProjectAchievements } from '../../core/api/api.types';

@Component({
  selector: 'app-achievements-card',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MatCardModule, MatIconModule],
  template: `
    @if (data(); as d) {
      <mat-card appearance="outlined" class="ach-card">
        <mat-card-header>
          <mat-card-title>Conquistas do time</mat-card-title>
          <mat-card-subtitle>
            Janela {{ d.windowDays }} dias · celebrações, não rankings
          </mat-card-subtitle>
        </mat-card-header>
        <mat-card-content>
          <div class="streak-row">
            @if (d.daysSinceLastIncident < 0) {
              <div class="streak-empty">
                <mat-icon class="streak-icon-muted" fontIcon="help"></mat-icon>
                <div>
                  <div class="streak-label">Sem incidents registrados</div>
                  <div class="muted">
                    Configure <code>jira_project_keys</code> e
                    <code>incident_jql</code> no projeto para começar a contar
                  </div>
                </div>
              </div>
            } @else {
              <div class="streak">
                <mat-icon class="streak-icon" fontIcon="local_fire_department"></mat-icon>
                <div>
                  <div class="streak-value">
                    {{ d.daysSinceLastIncident }}
                    <span class="streak-unit">
                      {{ d.daysSinceLastIncident === 1 ? 'dia' : 'dias' }}
                    </span>
                  </div>
                  <div class="streak-label">sem incident em produção</div>
                </div>
              </div>
            }
          </div>

          @if (d.achievements.length > 0) {
            <div class="badges">
              @for (a of d.achievements; track a.code) {
                <div class="badge" [title]="a.description">
                  <mat-icon class="badge-icon" [fontIcon]="a.icon "></mat-icon>
                  <div class="badge-body">
                    <div class="badge-title">{{ a.title }}</div>
                    <div class="badge-desc">{{ a.description }}</div>
                  </div>
                </div>
              }
            </div>
          } @else {
            <p class="muted no-badges">
              Sem conquistas desbloqueadas ainda. Continue gerando deploys
              estáveis — a próxima fica a 7 dias sem incident.
            </p>
          }
        </mat-card-content>
      </mat-card>
    }
  `,
  styles: [
    `
      :host {
        display: block;
        margin-top: var(--space-5);
      }
      .streak-row {
        display: flex;
        align-items: center;
        gap: var(--space-5);
        padding: var(--space-3) 0;
      }
      .streak {
        display: flex;
        align-items: center;
        gap: var(--space-3);
      }
      .streak-icon {
        font-size: 48px;
        height: 48px;
        width: 48px;
        color: #ea580c; /* fogo */
      }
      .streak-icon-muted {
        font-size: 36px;
        height: 36px;
        width: 36px;
        color: var(--color-text-muted);
      }
      .streak-empty {
        display: flex;
        align-items: center;
        gap: var(--space-3);
      }
      .streak-value {
        font-size: var(--font-size-3xl);
        font-weight: 700;
        line-height: 1;
        color: var(--color-text-primary);
        letter-spacing: -0.02em;
      }
      .streak-unit {
        font-size: var(--font-size-base);
        color: var(--color-text-secondary);
        font-weight: 500;
      }
      .streak-label {
        color: var(--color-text-secondary);
        font-size: var(--font-size-sm);
      }

      .badges {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
        gap: var(--space-3);
        margin-top: var(--space-4);
      }
      .badge {
        display: flex;
        align-items: flex-start;
        gap: var(--space-3);
        padding: var(--space-3);
        border-radius: var(--radius-md);
        background: var(--color-surface);
        border: 1px solid var(--color-border);
        transition: transform var(--transition-base), box-shadow var(--transition-fast);
      }
      .badge:hover {
        transform: translateY(-1px);
        box-shadow: var(--shadow-md);
      }
      .badge-icon {
        font-size: 28px;
        height: 28px;
        width: 28px;
        color: var(--color-brand);
        flex-shrink: 0;
      }
      .badge-title {
        font-weight: 600;
        color: var(--color-text-primary);
      }
      .badge-desc {
        font-size: var(--font-size-sm);
        color: var(--color-text-secondary);
        margin-top: 2px;
      }
      .no-badges {
        margin-top: var(--space-3);
      }
    `,
  ],
})
export class AchievementsCardComponent {
  data = input<ProjectAchievements | null>(null);
}
