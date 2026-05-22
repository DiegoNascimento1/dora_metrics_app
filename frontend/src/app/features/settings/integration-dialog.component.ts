import {
  ChangeDetectionStrategy,
  Component,
  inject,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import {
  MAT_DIALOG_DATA,
  MatDialogModule,
  MatDialogRef,
} from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';

import { ApiClient } from '../../core/api/api.client';
import {
  CreateSourceInstanceRequest,
  TestConnectionResponse,
} from '../../core/api/api.types';

export interface IntegrationDialogData {
  kind: 'gitlab' | 'jira';
  tenant: string;
}

@Component({
  selector: 'app-integration-dialog',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    MatDialogModule,
    MatFormFieldModule,
    MatInputModule,
    MatButtonModule,
    MatIconModule,
    MatProgressSpinnerModule,
  ],
  template: `
    <h2 mat-dialog-title>
      <span class="provider-mark" [class]="'provider-' + data.kind">
        {{ data.kind === 'gitlab' ? 'GitLab' : 'Jira' }}
      </span>
      Nova integração
    </h2>

    <mat-dialog-content class="content">
      <p class="muted">
        {{
          data.kind === 'gitlab'
            ? 'Cole um Personal Access Token (scope: read_api) da sua instância GitLab.'
            : 'Email da conta + API token (atlassian.com/account/security/api-tokens).'
        }}
      </p>

      <mat-form-field appearance="outline">
        <mat-label>Nome</mat-label>
        <input matInput [(ngModel)]="form.displayName" placeholder="ex: gitlab-prod" />
      </mat-form-field>

      <mat-form-field appearance="outline">
        <mat-label>URL base</mat-label>
        <input
          matInput
          type="url"
          [(ngModel)]="form.baseUrl"
          [placeholder]="
            data.kind === 'gitlab'
              ? 'https://gitlab.com'
              : 'https://acme.atlassian.net'
          "
        />
      </mat-form-field>

      @if (data.kind === 'jira') {
        <mat-form-field appearance="outline">
          <mat-label>Email Atlassian</mat-label>
          <input
            matInput
            type="email"
            [(ngModel)]="form.authEmail"
            placeholder="alice@acme.com"
          />
        </mat-form-field>
      }

      <mat-form-field appearance="outline">
        <mat-label>{{ data.kind === 'gitlab' ? 'Personal Access Token' : 'API Token' }}</mat-label>
        <input
          matInput
          [type]="showSecret() ? 'text' : 'password'"
          [(ngModel)]="form.secret"
          autocomplete="off"
          placeholder="glpat-xxxx ou ATATT3xFfGF0..."
        />
        <button
          mat-icon-button
          matSuffix
          type="button"
          (click)="showSecret.set(!showSecret())"
          [attr.aria-label]="showSecret() ? 'Esconder' : 'Mostrar'"
        >
          <mat-icon [fontIcon]="showSecret() ? 'visibility_off' : 'visibility' "></mat-icon>
        </button>
      </mat-form-field>

      @if (testResult(); as r) {
        <div class="test-result" [class.ok]="r.ok" [class.bad]="!r.ok">
          <mat-icon [fontIcon]="r.ok ? 'check_circle' : 'error' "></mat-icon>
          <div>
            <strong>{{ r.ok ? 'Conexão OK' : 'Falha' }}</strong>
            @if (r.message) {
              <div class="muted">{{ r.message }}</div>
            }
          </div>
        </div>
      }
    </mat-dialog-content>

    <mat-dialog-actions align="end" class="actions">
      <button mat-button mat-dialog-close type="button">Cancelar</button>

      <button
        mat-stroked-button
        type="button"
        (click)="testConnection()"
        [disabled]="!canTest() || testing()"
      >
        @if (testing()) {
          <mat-progress-spinner mode="indeterminate" diameter="16" />
        } @else {
          <mat-icon fontIcon="cloud_sync"></mat-icon>
        }
        Testar conexão
      </button>

      <button
        mat-flat-button
        color="primary"
        type="button"
        (click)="save()"
        [disabled]="!canSave() || saving()"
      >
        @if (saving()) {
          <mat-progress-spinner mode="indeterminate" diameter="16" />
        }
        Salvar integração
      </button>
    </mat-dialog-actions>
  `,
  styles: [
    `
      :host {
        display: block;
        min-width: 480px;
      }
      h2[mat-dialog-title] {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        font-weight: 700;
        letter-spacing: -0.01em;
      }
      .provider-mark {
        font-size: 0.75rem;
        font-weight: 600;
        padding: 4px 10px;
        border-radius: 999px;
        color: white;
        text-transform: lowercase;
        letter-spacing: 0.02em;
      }
      .provider-gitlab { background: var(--color-gitlab); }
      .provider-jira   { background: var(--color-jira); }

      .content {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        padding-top: var(--space-3) !important;
        padding-bottom: var(--space-2) !important;
      }
      .content > p {
        margin: 0 0 var(--space-3);
      }
      mat-form-field {
        width: 100%;
      }

      .test-result {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        padding: var(--space-3);
        border-radius: var(--radius-md);
        border: 1px solid var(--color-border);
      }
      .test-result.ok {
        background: var(--color-tier-elite-bg);
        border-color: color-mix(in srgb, var(--color-tier-elite) 30%, transparent);
        color: var(--color-tier-elite);
      }
      .test-result.bad {
        background: var(--color-tier-low-bg);
        border-color: color-mix(in srgb, var(--color-tier-low) 30%, transparent);
        color: var(--color-tier-low);
      }
      .actions {
        gap: var(--space-2);
        padding: var(--space-3) var(--space-5);
      }
      .actions mat-progress-spinner {
        margin-right: var(--space-2);
      }
    `,
  ],
})
export class IntegrationDialogComponent {
  private api = inject(ApiClient);
  private dialogRef = inject(MatDialogRef<IntegrationDialogComponent>);
  protected data = inject<IntegrationDialogData>(MAT_DIALOG_DATA);

  showSecret = signal(false);
  testing = signal(false);
  saving = signal(false);
  testResult = signal<TestConnectionResponse | null>(null);

  form: CreateSourceInstanceRequest = {
    tenant: this.data.tenant,
    kind: this.data.kind,
    baseUrl: this.data.kind === 'gitlab' ? 'https://gitlab.com' : '',
    displayName: '',
    secret: '',
    authEmail: '',
  };

  canTest(): boolean {
    if (!this.form.baseUrl || !this.form.secret) return false;
    if (this.data.kind === 'jira' && !this.form.authEmail) return false;
    return true;
  }

  canSave(): boolean {
    return this.canTest() && this.form.displayName.length > 0;
  }

  testConnection(): void {
    this.testing.set(true);
    this.testResult.set(null);
    this.api
      .testConnection({
        kind: this.data.kind,
        baseUrl: this.form.baseUrl,
        secret: this.form.secret,
        authEmail: this.form.authEmail,
      })
      .subscribe({
        next: (r) => {
          this.testResult.set(r);
          this.testing.set(false);
        },
        error: () => {
          this.testResult.set({ ok: false, message: 'Falha de rede' });
          this.testing.set(false);
        },
      });
  }

  save(): void {
    this.saving.set(true);
    this.api.createSourceInstance(this.form).subscribe({
      next: (created) => {
        this.saving.set(false);
        this.dialogRef.close(created);
      },
      error: (err) => {
        this.saving.set(false);
        this.testResult.set({
          ok: false,
          message: err?.error ?? err?.message ?? 'Erro ao salvar',
        });
      },
    });
  }
}
