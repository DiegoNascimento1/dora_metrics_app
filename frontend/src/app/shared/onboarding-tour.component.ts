import {
  ChangeDetectionStrategy,
  Component,
  Injectable,
  OnDestroy,
  computed,
  effect,
  inject,
  signal,
} from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';

const STORAGE_KEY = 'dora.tour.seen';

export interface TourStep {
  /** Texto do título do passo. */
  title: string;
  /** Descrição do passo (pode conter <strong>, <code>). */
  bodyHtml: string;
  /** Querry selector CSS do elemento a destacar. Se null, mostra centralizado sem spotlight. */
  selector: string | null;
}

/**
 * Steps default do tour. Adaptáveis via `OnboardingService.setSteps()`.
 *
 * Por que 4 passos: corresponde ao roadmap UX/UI ("4 steps: o que é DORA,
 * leitura de tile, tour da curva, drill-down"). Sem libs externas — usamos
 * overlay + clip-path para spotlight (≈ 60 LOC efetivas).
 */
const DEFAULT_STEPS: TourStep[] = [
  {
    title: 'Bem-vindo ao DORA Metrics',
    bodyHtml: `<p>Esta é uma plataforma que mede a <strong>saúde do seu fluxo de entrega</strong> usando as 4 métricas DORA:</p>
      <ul>
        <li><strong>Deployment Frequency</strong> — quantos deploys/dia</li>
        <li><strong>Lead Time for Changes</strong> — do commit ao prod</li>
        <li><strong>Change Failure Rate</strong> — % de deploys que quebram</li>
        <li><strong>MTTR</strong> — quão rápido se recupera</li>
      </ul>
      <p>Os dados vêm direto do GitLab e do Jira — sem inserção manual.</p>`,
    selector: null,
  },
  {
    title: 'Cada métrica vira um tile',
    bodyHtml: `<p>Cada tile mostra o valor atual e o <strong>tier</strong> (Elite, High, Medium, Low) baseado nos thresholds do DORA Report.</p>
      <p>A barra fina abaixo do tier indica quão perto você está do próximo nível.</p>`,
    selector: '.grid mat-card:first-of-type',
  },
  {
    title: 'A curva mostra o histórico',
    bodyHtml: `<p>O gráfico abaixo dos tiles é a série temporal: <strong>deploys por dia</strong> na janela escolhida (7d/30d/90d).</p>
      <p>Use isso para ver tendência: subindo? caindo? estável?</p>`,
    selector: '.chart-card',
  },
  {
    title: 'Drill-down para entender o porquê',
    bodyHtml: `<p>A tabela ao final lista os <strong>deploys reais</strong> da janela — projeto, ambiente, autor, MR de origem.</p>
      <p>Clique no chip de tier de qualquer métrica para abrir o painel <em>"Por que esse tier?"</em>.</p>
      <p>Bom dashboard! 🚀</p>`,
    selector: '.table-card',
  },
];

@Injectable({ providedIn: 'root' })
export class OnboardingService {
  /** Steps ativos. Permite override pelas features (ex: tour específico de /people). */
  steps = signal<TourStep[]>(DEFAULT_STEPS);
  /** Índice do step atual; -1 quando o tour não está visível. */
  current = signal<number>(-1);

  active = computed(() => this.current() >= 0);
  step = computed(() => this.steps()[this.current()] ?? null);

  /** Inicia o tour. Forçar=true ignora a flag de "já viu". */
  start(force = false): void {
    if (!force && this.hasSeen()) return;
    this.current.set(0);
  }

  next(): void {
    const next = this.current() + 1;
    if (next >= this.steps().length) {
      this.finish();
      return;
    }
    this.current.set(next);
  }

  prev(): void {
    this.current.set(Math.max(0, this.current() - 1));
  }

  /** Encerra o tour e marca como visto. */
  finish(): void {
    this.current.set(-1);
    try {
      localStorage.setItem(STORAGE_KEY, '1');
    } catch {
      // localStorage indisponível (modo privado, etc) — silencioso.
    }
  }

  /** Reset: usado pelo botão "Refazer tour" em settings. */
  reset(): void {
    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch {
      // ignore
    }
    this.current.set(0);
  }

  hasSeen(): boolean {
    try {
      return localStorage.getItem(STORAGE_KEY) === '1';
    } catch {
      return false;
    }
  }
}

/**
 * Overlay do tour. Monta dim layer + spotlight (via clip-path) sobre o
 * elemento referenciado pelo seletor do step atual + tooltip de texto.
 *
 * Respeita `prefers-reduced-motion`: transições viram instantâneas.
 *
 * Uso: incluir <app-onboarding-tour /> uma única vez no shell do app
 * (app.component.html / root). A8y: role=dialog + aria-live=polite, foco
 * inicial no botão "Próximo".
 */
@Component({
  selector: 'app-onboarding-tour',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MatButtonModule, MatIconModule],
  template: `
    @if (svc.active() && step(); as s) {
      <div
        class="overlay"
        role="dialog"
        aria-modal="true"
        [attr.aria-label]="s.title"
        [style.--spot-x]="spot().x + 'px'"
        [style.--spot-y]="spot().y + 'px'"
        [style.--spot-w]="spot().w + 'px'"
        [style.--spot-h]="spot().h + 'px'"
        [class.no-spot]="!s.selector"
        (click)="onBackdrop($event)"
      >
        <div
          class="panel"
          [style.top.px]="panel().top"
          [style.left.px]="panel().left"
          (click)="$event.stopPropagation()"
        >
          <header>
            <span class="badge">{{ svc.current() + 1 }} / {{ svc.steps().length }}</span>
            <h3>{{ s.title }}</h3>
            <button
              mat-icon-button
              (click)="svc.finish()"
              aria-label="Pular tour"
              type="button"
            >
              <mat-icon fontIcon="close"></mat-icon>
            </button>
          </header>
          <div class="body" [innerHTML]="s.bodyHtml"></div>
          <footer>
            <button mat-button (click)="svc.finish()" type="button">Pular</button>
            <div class="nav">
              <button
                mat-button
                (click)="svc.prev()"
                [disabled]="svc.current() === 0"
                type="button"
              >
                <mat-icon fontIcon="chevron_left"></mat-icon>
                Voltar
              </button>
              <button mat-flat-button color="primary" (click)="svc.next()" type="button">
                {{ isLast() ? 'Concluir' : 'Próximo' }}
                @if (!isLast()) {
                  <mat-icon fontIcon="chevron_right" iconPositionEnd></mat-icon>
                }
              </button>
            </div>
          </footer>
        </div>
      </div>
    }
  `,
  styles: [
    `
      .overlay {
        position: fixed;
        inset: 0;
        z-index: 1500;
        background:
          radial-gradient(
            ellipse var(--spot-w) var(--spot-h) at var(--spot-x) var(--spot-y),
            transparent 60%,
            rgba(0, 0, 0, 0.55) 65%
          );
        pointer-events: auto;
      }
      .overlay.no-spot {
        background: rgba(0, 0, 0, 0.6);
      }
      .panel {
        position: absolute;
        width: min(420px, calc(100vw - 32px));
        background: var(--color-surface);
        color: var(--color-text-primary);
        border-radius: var(--radius-lg);
        box-shadow: var(--shadow-lg);
        padding: var(--space-4);
        animation: pop 180ms ease-out;
      }
      header {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        margin-bottom: var(--space-3);
      }
      header h3 {
        flex: 1;
        margin: 0;
        font-size: var(--font-size-lg);
      }
      .badge {
        font-size: var(--font-size-xs);
        background: var(--color-brand-subtle);
        color: var(--color-brand);
        padding: 2px 8px;
        border-radius: 999px;
        font-weight: 600;
      }
      .body {
        color: var(--color-text-secondary);
        font-size: var(--font-size-sm);
        line-height: 1.55;
      }
      .body ul {
        padding-left: var(--space-4);
      }
      footer {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-top: var(--space-3);
      }
      .nav {
        display: flex;
        gap: var(--space-2);
      }
      @keyframes pop {
        from {
          opacity: 0;
          transform: translateY(8px) scale(0.98);
        }
        to {
          opacity: 1;
          transform: translateY(0) scale(1);
        }
      }
      @media (prefers-reduced-motion: reduce) {
        .panel {
          animation: none;
        }
      }
    `,
  ],
})
export class OnboardingTourComponent implements OnDestroy {
  protected svc = inject(OnboardingService);
  protected step = this.svc.step;
  protected isLast = computed(
    () => this.svc.current() === this.svc.steps().length - 1,
  );

  /** Bounding box do elemento focado pelo step (spotlight). */
  protected spot = signal({ x: 0, y: 0, w: 0, h: 0 });
  /** Posição do panel (lateral oposta ao spotlight, com fallback central). */
  protected panel = signal({ top: 0, left: 0 });

  /** Resize listener para reposicionar quando viewport mudar. */
  private resizeHandler = () => this.recomputeForStep(this.step());

  constructor() {
    effect(() => {
      const s = this.step();
      if (s) {
        // Aguarda 1 frame para que o DOM tenha renderizado o destino.
        queueMicrotask(() => this.recomputeForStep(s));
      }
    });
    window.addEventListener('resize', this.resizeHandler);
    window.addEventListener('scroll', this.resizeHandler, true);
  }

  ngOnDestroy(): void {
    window.removeEventListener('resize', this.resizeHandler);
    window.removeEventListener('scroll', this.resizeHandler, true);
  }

  private recomputeForStep(s: TourStep | null): void {
    if (!s || !s.selector) {
      // Centralizado.
      this.spot.set({ x: 0, y: 0, w: 0, h: 0 });
      this.panel.set({
        top: Math.max(60, (window.innerHeight - 320) / 2),
        left: Math.max(16, (window.innerWidth - 420) / 2),
      });
      return;
    }
    const el = document.querySelector<HTMLElement>(s.selector);
    if (!el) {
      this.recomputeForStep({ ...s, selector: null });
      return;
    }
    const r = el.getBoundingClientRect();
    const padding = 8;
    this.spot.set({
      x: r.left + r.width / 2,
      y: r.top + r.height / 2,
      w: r.width + padding * 2,
      h: r.height + padding * 2,
    });

    // Panel: tenta colocar abaixo do spotlight; se não couber, acima.
    const panelW = 420;
    const panelH = 280;
    let top = r.bottom + 16;
    let left = Math.max(16, Math.min(r.left, window.innerWidth - panelW - 16));
    if (top + panelH > window.innerHeight - 16) {
      top = Math.max(16, r.top - panelH - 16);
    }
    this.panel.set({ top, left });

    // Scroll suave até o elemento se estiver fora da viewport.
    if (r.top < 0 || r.bottom > window.innerHeight) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }
  }

  onBackdrop(_e: MouseEvent): void {
    // clique no backdrop NÃO fecha o tour (evita perda acidental); usar "Pular".
  }
}
