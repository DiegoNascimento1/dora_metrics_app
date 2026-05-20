import { Injectable, computed, signal } from '@angular/core';

export type ThemeMode = 'light' | 'dark' | 'system';

const STORAGE_KEY = 'dora-theme';
const DARK_ATTR = 'dark';

/**
 * Gerencia o tema (light/dark/system) com persistência em localStorage
 * e sincronização com prefers-color-scheme.
 *
 * O modo "system" segue o OS e é o default. Quando o usuário escolhe
 * explicitamente light ou dark, a escolha persiste entre sessões.
 *
 * O atributo final no `<html>` é `data-theme="dark"` (ou ausente). Os tokens
 * em src/styles/_tokens.scss reagem a esse atributo.
 */
@Injectable({ providedIn: 'root' })
export class ThemeService {
  private readonly mediaQuery: MediaQueryList | null =
    typeof window !== 'undefined' && window.matchMedia
      ? window.matchMedia('(prefers-color-scheme: dark)')
      : null;

  private readonly _mode = signal<ThemeMode>(this.readStoredMode());
  readonly mode = this._mode.asReadonly();

  /** Se o tema efetivo atual é dark (resolvendo "system"). */
  readonly isDark = computed(() => {
    const m = this._mode();
    if (m === 'dark') return true;
    if (m === 'light') return false;
    return this.mediaQuery?.matches ?? false;
  });

  constructor() {
    this.applyToDocument();

    // Reagimos a mudanças do prefers-color-scheme apenas quando estamos em modo "system".
    if (this.mediaQuery) {
      this.mediaQuery.addEventListener('change', () => {
        if (this._mode() === 'system') {
          this.applyToDocument();
        }
      });
    }
  }

  /** Alterna entre light e dark; ignora "system" (sai para o oposto do atual). */
  toggle(): void {
    this.set(this.isDark() ? 'light' : 'dark');
  }

  /** Define explicitamente um modo e persiste. */
  set(mode: ThemeMode): void {
    this._mode.set(mode);
    try {
      if (mode === 'system') {
        localStorage.removeItem(STORAGE_KEY);
      } else {
        localStorage.setItem(STORAGE_KEY, mode);
      }
    } catch {
      // Ignore (modo privado, etc.).
    }
    this.applyToDocument();
  }

  private readStoredMode(): ThemeMode {
    try {
      const v = localStorage.getItem(STORAGE_KEY);
      if (v === 'light' || v === 'dark') return v;
    } catch {
      // ignore
    }
    return 'system';
  }

  private applyToDocument(): void {
    if (typeof document === 'undefined') return;
    const html = document.documentElement;
    if (this.isDark()) {
      html.setAttribute('data-theme', DARK_ATTR);
    } else {
      html.removeAttribute('data-theme');
    }
  }
}
