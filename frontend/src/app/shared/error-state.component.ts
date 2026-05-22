import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { MatIconModule } from '@angular/material/icon';

/**
 * Variantes do <app-error-state>. Mapeiam para um ícone Material Symbol +
 * tonalidade (status warning/error) sem hardcode de cor: cada variante
 * referencia tokens em _tokens.scss.
 */
export type ErrorStateVariant = 'network' | 'not-found' | 'forbidden' | 'generic';

interface VariantPreset {
  icon: string;
  /** Cor primária do círculo (var() token). */
  tone: 'warning' | 'danger' | 'muted';
}

const VARIANT_PRESETS: Record<ErrorStateVariant, VariantPreset> = {
  network: { icon: 'cloud_off', tone: 'warning' },
  'not-found': { icon: 'search_off', tone: 'muted' },
  forbidden: { icon: 'lock', tone: 'danger' },
  generic: { icon: 'error_outline', tone: 'danger' },
};

/**
 * <app-error-state> — espelho do <app-empty-state> dedicado a falhas.
 *
 * Diferenças vs empty-state:
 *  - Ícone reflete tipo de erro (rede, 403, 404, genérico)
 *  - Tom de cor varia: warning (network/transitório) / danger (forbidden,
 *    generic) / muted (not-found)
 *  - Mantém slot CTA — comum: "Tentar de novo", "Voltar ao dashboard"
 *
 * Acessibilidade: role="alert" + aria-live="polite" para que leitores de
 * tela anunciem a mudança quando o componente substitui o conteúdo.
 */
@Component({
  selector: 'app-error-state',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MatIconModule],
  template: `
    <div class="error" role="alert" aria-live="polite" [attr.data-tone]="preset().tone">
      <div class="icon-wrap">
        <mat-icon class="icon" [fontIcon]="iconOverride() || preset().icon"></mat-icon>
      </div>
      <h3 class="title">{{ title() }}</h3>
      @if (description()) {
        <p class="desc">{{ description() }}</p>
      }
      @if (detail()) {
        <details class="detail">
          <summary>Detalhes técnicos</summary>
          <pre>{{ detail() }}</pre>
        </details>
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
      .error {
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
        display: flex;
        align-items: center;
        justify-content: center;
      }
      [data-tone='warning'] .icon-wrap {
        background: var(--color-tier-medium-bg);
      }
      [data-tone='warning'] .icon {
        color: var(--color-tier-medium);
      }
      [data-tone='danger'] .icon-wrap {
        background: var(--color-tier-low-bg);
      }
      [data-tone='danger'] .icon {
        color: var(--color-tier-low);
      }
      [data-tone='muted'] .icon-wrap {
        background: var(--color-tier-na-bg);
      }
      [data-tone='muted'] .icon {
        color: var(--color-tier-na);
      }
      .icon {
        font-size: 32px;
        height: 32px;
        width: 32px;
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
      .detail {
        margin: 0 0 var(--space-4);
        text-align: left;
        color: var(--color-text-muted);
        font-size: var(--font-size-sm);
      }
      .detail summary {
        cursor: pointer;
        color: var(--color-text-secondary);
        padding: var(--space-1) 0;
      }
      .detail pre {
        background: var(--color-surface-subtle);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-sm);
        padding: var(--space-2);
        overflow-x: auto;
        font-size: 0.8rem;
      }
      .cta {
        display: flex;
        justify-content: center;
        gap: var(--space-2);
        flex-wrap: wrap;
      }
    `,
  ],
})
export class ErrorStateComponent {
  variant = input<ErrorStateVariant>('generic');
  title = input.required<string>();
  description = input<string>('');
  /** Opcional: mensagem bruta do backend, exibida em <details> recolhido. */
  detail = input<string>('');
  /** Override raro: força um ícone diferente do default da variante. */
  iconOverride = input<string>('');

  protected preset = computed<VariantPreset>(() => VARIANT_PRESETS[this.variant()]);
}
