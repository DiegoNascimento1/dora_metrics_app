import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { MatIconModule } from '@angular/material/icon';

/**
 * <app-empty-state> — estado vazio desenhado: ícone grande + título +
 * descrição + slot pra ação (CTA).
 *
 * Padrão Material 3 + corporativo: ícone em círculo subtle, sem
 * ilustrações 3D (anti-padrão registrado no roadmap).
 */
@Component({
  selector: 'app-empty-state',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MatIconModule],
  template: `
    <div class="empty">
      <div class="icon-wrap">
        <mat-icon class="icon">{{ icon() }}</mat-icon>
      </div>
      <h3 class="title">{{ title() }}</h3>
      @if (description()) {
        <p class="desc">{{ description() }}</p>
      }
      <div class="cta">
        <ng-content />
      </div>
    </div>
  `,
  styles: [
    `
      :host {
        display: block;
      }
      .empty {
        text-align: center;
        padding: var(--space-6) var(--space-4);
        max-width: 480px;
        margin: 0 auto;
      }
      .icon-wrap {
        width: 64px;
        height: 64px;
        margin: 0 auto var(--space-4);
        border-radius: 50%;
        background: var(--color-brand-subtle);
        display: flex;
        align-items: center;
        justify-content: center;
      }
      .icon {
        font-size: 32px;
        height: 32px;
        width: 32px;
        color: var(--color-brand);
      }
      .title {
        margin: 0 0 var(--space-2);
        font-size: var(--font-size-lg);
        font-weight: 600;
        color: var(--color-text-primary);
      }
      .desc {
        margin: 0 0 var(--space-4);
        color: var(--color-text-secondary);
        line-height: 1.5;
      }
      .cta {
        display: flex;
        justify-content: center;
        gap: var(--space-2);
      }
    `,
  ],
})
export class EmptyStateComponent {
  icon = input<string>('inbox');
  title = input.required<string>();
  description = input<string>('');
}
