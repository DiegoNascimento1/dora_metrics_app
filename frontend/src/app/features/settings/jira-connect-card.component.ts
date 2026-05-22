import {
  ChangeDetectionStrategy,
  Component,
  OnInit,
  inject,
  input,
  signal,
} from '@angular/core';
import { DatePipe } from '@angular/common';
import { MatCardModule } from '@angular/material/card';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatSnackBar } from '@angular/material/snack-bar';
import { ActivatedRoute, Router } from '@angular/router';
import { catchError, finalize, of } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import { AtlassianStatus } from '../../core/api/api.types';

/**
 * Card "Conectar Jira" (Atlassian Rovo MCP via OAuth 3LO).
 *
 * Estados:
 *   - available=false → backend não configurou ATLASSIAN_OAUTH_CLIENT_ID:
 *     mostra explicação do que o admin precisa fazer no Developer Console.
 *   - connected=false → botão "Conectar Jira" (chama /authorize, redireciona).
 *   - connected=true → mostra site, scopes, expira-em, último refresh,
 *     botão "Desconectar".
 *
 * Lida com `?atlassian=connected` e `?atlassian_error=...` na URL pós-redirect
 * para mostrar snackbar e limpar query string.
 */
@Component({
  selector: 'app-jira-connect-card',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    DatePipe,
    MatCardModule,
    MatButtonModule,
    MatIconModule,
  ],
  template: `
    <mat-card appearance="outlined" class="connect">
      <mat-card-header>
        <mat-card-title>
          <mat-icon class="head" fontIcon="link"></mat-icon>
          Conectar Jira (Atlassian Rovo)
        </mat-card-title>
        <mat-card-subtitle>
          Conecta a conta Atlassian do tenant <strong>{{ tenant() }}</strong> via OAuth 3LO. Tokens ficam criptografados no servidor; o coletor renova automaticamente.
        </mat-card-subtitle>
      </mat-card-header>

      <mat-card-content>
        @if (status(); as st) {
          @if (!st.available) {
            <p class="muted">
              <mat-icon fontIcon="info" class="info-icon"></mat-icon>
              Feature desligada no backend: <code>ATLASSIAN_OAUTH_CLIENT_ID</code>
              + <code>ATLASSIAN_OAUTH_CLIENT_SECRET</code> + <code>OAUTH_ENCRYPTION_KEY</code>
              precisam estar configurados. O coletor cai para REST com env tradicional.
            </p>
            <p class="muted">
              Reason: <code>{{ st.reason }}</code>
            </p>
          } @else if (st.connected && st.connection) {
            <div class="kv">
              <div><strong>Site</strong> <code>{{ st.connection.siteUrl || '—' }}</code></div>
              <div><strong>Cloud ID</strong> <code>{{ st.connection.cloudId || '—' }}</code></div>
              <div><strong>Scopes</strong> <span>{{ st.connection.scope || '—' }}</span></div>
              <div>
                <strong>Token expira em</strong>
                <span>{{ st.connection.expiresAt | date: 'short' }}</span>
              </div>
              @if (st.connection.lastRefreshedAt) {
                <div>
                  <strong>Último refresh</strong>
                  <span>{{ st.connection.lastRefreshedAt | date: 'short' }}</span>
                </div>
              }
              @if (st.connection.lastRefreshError) {
                <div class="error">
                  <strong>Erro no último refresh</strong>
                  <span>{{ st.connection.lastRefreshError }}</span>
                </div>
              }
              <div>
                <strong>Conectado em</strong>
                <span>{{ st.connection.connectedAt | date: 'short' }}{{ st.connection.connectedBy ? ' por ' + st.connection.connectedBy : '' }}</span>
              </div>
            </div>
          } @else {
            <p>
              Ao conectar, você será redirecionado para o Atlassian para autorizar a leitura de issues
              (<code>read:jira-work</code>, <code>read:jira-user</code>, <code>offline_access</code>).
              Tokens não trafegam pelo browser — o backend os salva criptografados.
            </p>
          }
        } @else if (loading()) {
          <p class="muted">Carregando status...</p>
        }
      </mat-card-content>

      <mat-card-actions align="end">
        @if (status(); as st) {
          @if (st.available && st.connected) {
            <button mat-stroked-button (click)="disconnect()" [disabled]="busy()">
              <mat-icon fontIcon="link_off"></mat-icon>
              Desconectar
            </button>
          } @else if (st.available && !st.connected) {
            <button mat-flat-button color="primary" (click)="connect()" [disabled]="busy()">
              <mat-icon fontIcon="login"></mat-icon>
              Conectar Jira
            </button>
          }
        }
      </mat-card-actions>
    </mat-card>
  `,
  styles: [
    `
      :host { display: block; margin-top: var(--space-3); }
      .connect mat-card-title {
        display: inline-flex; align-items: center; gap: var(--space-2);
      }
      .head { color: var(--color-brand); }
      .info-icon {
        font-size: 16px; height: 16px; width: 16px; vertical-align: middle;
      }
      .muted { color: var(--color-text-secondary); }
      .kv {
        display: grid;
        gap: var(--space-2);
        grid-template-columns: 1fr;
        margin-top: var(--space-2);
      }
      .kv > div {
        display: grid;
        grid-template-columns: 180px 1fr;
        align-items: baseline;
        gap: var(--space-2);
      }
      .kv strong { color: var(--color-text-secondary); font-weight: 600; }
      .kv code {
        font-family: var(--font-mono);
        font-size: 0.85em;
        background: var(--color-surface-subtle);
        padding: 1px 6px;
        border-radius: var(--radius-sm);
      }
      .error { color: var(--color-tier-low); }
      .error strong { color: var(--color-tier-low); }
    `,
  ],
})
export class JiraConnectCardComponent implements OnInit {
  private api = inject(ApiClient);
  private snack = inject(MatSnackBar);
  private route = inject(ActivatedRoute);
  private router = inject(Router);

  tenant = input.required<string>();

  status = signal<AtlassianStatus | null>(null);
  loading = signal(false);
  busy = signal(false);

  ngOnInit(): void {
    // Lê query params do redirect OAuth.
    const qp = this.route.snapshot.queryParams;
    if (qp['atlassian'] === 'connected') {
      this.snack.open('✅ Jira conectado!', '', { duration: 4000 });
      this.cleanQuery();
    } else if (qp['atlassian_error']) {
      this.snack.open(`Falha ao conectar Jira: ${qp['atlassian_error']}`, 'OK', { duration: 6000 });
      this.cleanQuery();
    }

    this.refresh();
  }

  refresh(): void {
    this.loading.set(true);
    this.api
      .getAtlassianStatus(this.tenant())
      .pipe(
        catchError((err) => {
          this.snack.open(`Erro ao consultar status: ${err.message}`, 'OK', { duration: 5000 });
          return of<AtlassianStatus | null>(null);
        }),
        finalize(() => this.loading.set(false)),
      )
      .subscribe((s) => this.status.set(s));
  }

  connect(): void {
    this.busy.set(true);
    const returnTo = window.location.pathname; // ex: "/settings"
    this.api
      .startAtlassianAuthorize(this.tenant(), returnTo)
      .pipe(
        catchError((err) => {
          this.snack.open(`Erro: ${err.message}`, 'OK', { duration: 5000 });
          this.busy.set(false);
          return of(null);
        }),
      )
      .subscribe((res) => {
        if (res?.authorizeUrl) {
          // Navega para fora do app — atlassian volta para /callback.
          window.location.href = res.authorizeUrl;
        }
      });
  }

  disconnect(): void {
    if (!confirm('Desconectar o Jira deste tenant? Tokens locais serão apagados.')) return;
    this.busy.set(true);
    this.api
      .disconnectAtlassian(this.tenant())
      .pipe(
        catchError((err) => {
          this.snack.open(`Erro: ${err.message}`, 'OK', { duration: 5000 });
          return of(null);
        }),
        finalize(() => this.busy.set(false)),
      )
      .subscribe(() => {
        this.snack.open('Jira desconectado.', '', { duration: 3000 });
        this.refresh();
      });
  }

  private cleanQuery(): void {
    void this.router.navigate([], {
      queryParams: { atlassian: null, atlassian_error: null },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    });
  }
}
