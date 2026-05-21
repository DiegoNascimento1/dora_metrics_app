import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/**
 * <app-skeleton> — placeholder de carregamento com shimmer suave.
 *
 * Usado no lugar do mat-spinner pra que a UI mantenha forma + ritmo
 * visual enquanto os dados carregam. Respeita prefers-reduced-motion.
 *
 * Variantes:
 *   variant="text"   — linha de texto (altura default = 1em)
 *   variant="title"  — h1/h2 mais alto
 *   variant="card"   — bloco de card; respeita height
 *   variant="circle" — avatar/icon redondo
 *   variant="chip"   — chip/tier badge
 */
@Component({
  selector: 'app-skeleton',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<span class="sk" [class]="'sk-' + variant()" [style.width]="width()" [style.height]="height()"></span>`,
  styles: [
    `
      :host {
        display: contents;
      }
      .sk {
        display: block;
        background: linear-gradient(
          90deg,
          var(--color-surface-subtle) 0%,
          var(--color-border) 50%,
          var(--color-surface-subtle) 100%
        );
        background-size: 200% 100%;
        animation: sk-shimmer 1.6s ease-in-out infinite;
        border-radius: var(--radius-sm);
      }
      .sk-text   { height: 1em; border-radius: 4px; }
      .sk-title  { height: 1.75rem; border-radius: 6px; }
      .sk-card   { width: 100%; height: 96px; border-radius: var(--radius-lg); }
      .sk-circle { border-radius: 50%; }
      .sk-chip   { height: 24px; border-radius: 999px; }

      @keyframes sk-shimmer {
        0%   { background-position: 200% 0; }
        100% { background-position: -200% 0; }
      }

      @media (prefers-reduced-motion: reduce) {
        .sk { animation: none; opacity: 0.6; }
      }
    `,
  ],
})
export class SkeletonComponent {
  variant = input<'text' | 'title' | 'card' | 'circle' | 'chip'>('text');
  width = input<string>('100%');
  height = input<string>('');
}
