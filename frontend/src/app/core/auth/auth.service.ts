import { Injectable, inject, signal } from '@angular/core';
import { OidcSecurityService } from 'angular-auth-oidc-client';
import { Observable, of } from 'rxjs';
import { map, tap } from 'rxjs/operators';

import { getRuntimeAuthConfig } from './auth.config';

/**
 * Facade da lib angular-auth-oidc-client. O resto do app injeta
 * `AuthService` em vez de `OidcSecurityService` diretamente — assim
 * podemos trocar de lib no futuro sem repassar pelo código todo.
 *
 * Quando `auth.enabled = false` (default em dev), todos os métodos
 * devolvem placeholders permitindo o app rodar sem IdP configurado.
 */
@Injectable({ providedIn: 'root' })
export class AuthService {
  private oidc = inject(OidcSecurityService, { optional: true });
  private cfg = getRuntimeAuthConfig();

  /** `true` quando OIDC está ativo e o usuário está autenticado. */
  isAuthenticated = signal(false);
  /** Email/sub do usuário corrente; vazio se OIDC desligado. */
  username = signal<string>('');

  get enabled(): boolean {
    return this.cfg.enabled;
  }

  /** Chamar no app start para inicializar a sessão. */
  initialize(): Observable<boolean> {
    if (!this.enabled || !this.oidc) {
      return of(true);
    }
    return this.oidc.checkAuth().pipe(
      tap((res) => {
        this.isAuthenticated.set(res.isAuthenticated);
        const claims = res.userData as { email?: string; preferred_username?: string; sub?: string } | null;
        this.username.set(claims?.email || claims?.preferred_username || claims?.sub || '');
      }),
      map((res) => res.isAuthenticated),
    );
  }

  login(): void {
    if (!this.enabled || !this.oidc) return;
    this.oidc.authorize();
  }

  logout(): void {
    if (!this.enabled || !this.oidc) return;
    this.oidc.logoff().subscribe();
    this.isAuthenticated.set(false);
    this.username.set('');
  }

  /** Devolve o access token corrente para anexar no Authorization header. */
  getAccessToken(): Observable<string> {
    if (!this.enabled || !this.oidc) return of('');
    return this.oidc.getAccessToken();
  }
}
