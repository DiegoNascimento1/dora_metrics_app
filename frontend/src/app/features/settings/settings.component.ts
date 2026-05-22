import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DatePipe } from '@angular/common';
import { MatCardModule } from '@angular/material/card';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatDialog, MatDialogModule } from '@angular/material/dialog';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { catchError, finalize, of } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import { SourceInstance } from '../../core/api/api.types';
import { SkeletonComponent } from '../../shared/skeleton.component';
import { EmptyStateComponent } from '../../shared/empty-state.component';
import {
  IntegrationDialogComponent,
  IntegrationDialogData,
} from './integration-dialog.component';

@Component({
  selector: 'app-settings',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    DatePipe,
    MatCardModule,
    MatButtonModule,
    MatIconModule,
    MatFormFieldModule,
    MatInputModule,
    MatDialogModule,
    MatSnackBarModule,
    SkeletonComponent,
    EmptyStateComponent,
  ],
  template: `
    <h1>Configurações</h1>

    <div class="filters">
      <mat-form-field appearance="outline">
        <mat-label>Tenant</mat-label>
        <input matInput [(ngModel)]="tenant" (change)="reload()" placeholder="acme" />
      </mat-form-field>
      <button mat-stroked-button (click)="reload()">
        <mat-icon fontIcon="refresh"></mat-icon>
        Atualizar
      </button>
    </div>

    @if (loading()) {
      <div class="grid">
        @for (_ of [0, 1]; track $index) {
          <mat-card appearance="outlined" class="skel-card">
            <app-skeleton variant="chip" width="60px" />
            <app-skeleton variant="title" width="50%" />
            <app-skeleton variant="text" width="80%" />
            <app-skeleton variant="card" height="64px" />
          </mat-card>
        }
      </div>
    } @else {
      <div class="grid">
        <mat-card appearance="outlined" class="integration-card">
          <mat-card-header>
            <div class="card-head">
              <div>
                <mat-card-title>
                  <span class="provider-mark provider-gitlab">GitLab</span>
                </mat-card-title>
                <mat-card-subtitle>
                  Coleta de deployments + merge requests
                </mat-card-subtitle>
              </div>
              <button
                mat-flat-button
                color="primary"
                (click)="addIntegration('gitlab')"
              >
                <mat-icon fontIcon="add"></mat-icon>
                Conectar GitLab
              </button>
            </div>
          </mat-card-header>
          <mat-card-content>
            @if (gitlabInstances().length === 0) {
              <app-empty-state
                icon="cable"
                title="Nenhum GitLab conectado"
                description="Adicione uma instância para começar a coletar deployments e merge requests."
              />
            } @else {
              @for (i of gitlabInstances(); track i.id) {
                <div class="integration-row">
                  <div class="integration-info">
                    <strong>{{ i.displayName }}</strong>
                    <div class="muted"><code>{{ i.baseUrl }}</code></div>
                    <div class="muted-2">
                      <mat-icon class="row-icon" fontIcon="vpn_key"></mat-icon>
                      {{ i.hasSecret ? 'Token armazenado' : 'Usa env var (' + i.authRef + ')' }}
                      · criado {{ i.createdAt | date: 'short' }}
                    </div>
                  </div>
                  <button
                    mat-icon-button
                    color="warn"
                    (click)="removeIntegration(i)"
                    [attr.aria-label]="'Remover ' + i.displayName"
                  >
                    <mat-icon fontIcon="delete_outline"></mat-icon>
                  </button>
                </div>
              }
            }
          </mat-card-content>
        </mat-card>

        <mat-card appearance="outlined" class="integration-card">
          <mat-card-header>
            <div class="card-head">
              <div>
                <mat-card-title>
                  <span class="provider-mark provider-jira">Jira</span>
                </mat-card-title>
                <mat-card-subtitle>
                  Coleta de incidents para CFR + MTTR
                </mat-card-subtitle>
              </div>
              <button
                mat-flat-button
                color="primary"
                (click)="addIntegration('jira')"
              >
                <mat-icon fontIcon="add"></mat-icon>
                Conectar Jira
              </button>
            </div>
          </mat-card-header>
          <mat-card-content>
            @if (jiraInstances().length === 0) {
              <app-empty-state
                icon="cable"
                title="Nenhum Jira conectado"
                description="Adicione um site Atlassian para começar a coletar incidents (Change Failure Rate + MTTR)."
              />
            } @else {
              @for (i of jiraInstances(); track i.id) {
                <div class="integration-row">
                  <div class="integration-info">
                    <strong>{{ i.displayName }}</strong>
                    <div class="muted"><code>{{ i.baseUrl }}</code></div>
                    <div class="muted-2">
                      <mat-icon class="row-icon" fontIcon="person"></mat-icon>
                      {{ i.authEmail || '—' }} ·
                      <mat-icon class="row-icon" fontIcon="vpn_key"></mat-icon>
                      {{ i.hasSecret ? 'Token armazenado' : 'Usa env var' }}
                      · criado {{ i.createdAt | date: 'short' }}
                    </div>
                  </div>
                  <button
                    mat-icon-button
                    color="warn"
                    (click)="removeIntegration(i)"
                    [attr.aria-label]="'Remover ' + i.displayName"
                  >
                    <mat-icon fontIcon="delete_outline"></mat-icon>
                  </button>
                </div>
              }
            }
          </mat-card-content>
        </mat-card>
      </div>

      <mat-card appearance="outlined" class="info-card">
        <mat-card-content>
          <h2>Sobre as credenciais</h2>
          <p class="muted">
            Tokens são armazenados <strong>diretamente no banco</strong> da
            plataforma. Para deployments de produção, considere mover para um
            secret manager (HashiCorp Vault, AWS Secrets Manager) editando
            <code>internal/secret/provider.go</code>.
          </p>
          <p class="muted">
            A integração legada via variável de ambiente
            (<code>GITLAB_TOKEN</code> / <code>JIRA_API_TOKEN</code>) continua
            funcionando — instâncias criadas via CLI são preservadas.
          </p>
        </mat-card-content>
      </mat-card>
    }
  `,
  styles: [
    `
      .filters {
        display: flex;
        align-items: center;
        gap: var(--space-4);
        margin: var(--space-4) 0;
      }
      .grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(420px, 1fr));
        gap: var(--space-4);
      }

      .integration-card {
        display: flex;
        flex-direction: column;
      }
      .card-head {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        gap: var(--space-4);
        width: 100%;
      }

      .provider-mark {
        font-size: 0.75rem;
        font-weight: 700;
        padding: 4px 12px;
        border-radius: 999px;
        color: white;
        text-transform: lowercase;
        letter-spacing: 0.02em;
        display: inline-block;
      }
      .provider-gitlab { background: var(--color-gitlab); }
      .provider-jira   { background: var(--color-jira); }

      .empty {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        padding: var(--space-5) 0;
        color: var(--color-text-muted);
      }
      .empty-icon {
        font-size: 28px;
        height: 28px;
        width: 28px;
      }

      .integration-row {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: var(--space-3);
        padding: var(--space-3) 0;
        border-bottom: 1px solid var(--color-border);
      }
      .integration-row:last-child {
        border-bottom: none;
      }
      .integration-info {
        display: flex;
        flex-direction: column;
        gap: 4px;
      }
      .integration-info code {
        background: var(--color-surface-subtle);
        padding: 2px 6px;
        border-radius: var(--radius-sm);
        font-size: 0.8125rem;
      }
      .muted-2 {
        display: flex;
        align-items: center;
        gap: 6px;
        color: var(--color-text-muted);
        font-size: 0.8125rem;
      }
      .row-icon {
        font-size: 16px;
        height: 16px;
        width: 16px;
        opacity: 0.7;
      }

      .info-card {
        margin-top: var(--space-5);
      }
      .info-card h2 {
        margin: 0 0 var(--space-2);
        font-size: var(--font-size-lg);
      }
      .info-card p {
        margin: 0 0 var(--space-2);
      }
      .info-card code {
        background: var(--color-surface-subtle);
        padding: 2px 6px;
        border-radius: var(--radius-sm);
        font-family: var(--font-mono);
        font-size: 0.85em;
      }
      .skel-card {
        padding: var(--space-4);
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
    `,
  ],
})
export class SettingsComponent {
  private api = inject(ApiClient);
  private dialog = inject(MatDialog);
  private snack = inject(MatSnackBar);

  tenant = 'acme';
  loading = signal(false);
  instances = signal<SourceInstance[]>([]);

  gitlabInstances = computed(() =>
    this.instances().filter((i) => i.kind === 'gitlab'),
  );
  jiraInstances = computed(() =>
    this.instances().filter((i) => i.kind === 'jira'),
  );

  constructor() {
    this.reload();
  }

  reload(): void {
    if (!this.tenant) return;
    this.loading.set(true);
    this.api
      .listSourceInstances(this.tenant)
      .pipe(
        catchError(() => of([] as SourceInstance[])),
        finalize(() => this.loading.set(false)),
      )
      .subscribe((rows) => this.instances.set(rows));
  }

  addIntegration(kind: 'gitlab' | 'jira'): void {
    const data: IntegrationDialogData = { kind, tenant: this.tenant };
    const ref = this.dialog.open(IntegrationDialogComponent, {
      data,
      width: 'min(560px, 92vw)',
      autoFocus: 'first-tabbable',
    });
    ref.afterClosed().subscribe((created) => {
      if (created) {
        this.snack.open(
          `Integração ${kind} criada: ${created.displayName}`,
          'OK',
          { duration: 3500 },
        );
        this.reload();
      }
    });
  }

  removeIntegration(i: SourceInstance): void {
    if (!confirm(`Remover integração ${i.displayName}? Os projetos vinculados também serão removidos.`)) {
      return;
    }
    this.api.deleteSourceInstance(i.id).subscribe({
      next: () => {
        this.snack.open(`Integração ${i.displayName} removida`, 'OK', {
          duration: 3500,
        });
        this.reload();
      },
      error: (err) => {
        this.snack.open(
          `Erro: ${err?.error ?? err?.message ?? err}`,
          'OK',
          { duration: 5000 },
        );
      },
    });
  }
}
