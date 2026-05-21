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
import { MatSelectModule } from '@angular/material/select';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';

import { ApiClient } from '../../core/api/api.client';
import {
  AlertKind,
  AlertRule,
  AlertScopeKind,
  CreateAlertRuleRequest,
} from '../../core/api/api.types';

export interface AlertRuleDialogData {
  tenant: string;
  rule?: AlertRule;
}

@Component({
  selector: 'app-alert-rule-dialog',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    MatDialogModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatButtonModule,
    MatIconModule,
    MatSlideToggleModule,
    MatProgressSpinnerModule,
  ],
  template: `
    <h2 mat-dialog-title>
      <mat-icon class="title-icon">notifications_active</mat-icon>
      {{ data.rule ? 'Editar regra' : 'Nova regra de alerta' }}
    </h2>

    <mat-dialog-content class="content">
      <p class="muted">
        Dispara um webhook quando a classificação DORA combinada do escopo
        observado mudar (regressão ou qualquer mudança).
      </p>

      <mat-form-field appearance="outline">
        <mat-label>Nome</mat-label>
        <input
          matInput
          [(ngModel)]="form.name"
          placeholder="ex: alerta-prod-regressao"
        />
      </mat-form-field>

      <div class="row">
        <mat-form-field appearance="outline" class="grow">
          <mat-label>Tipo de gatilho</mat-label>
          <mat-select [(ngModel)]="form.kind">
            <mat-option value="tier_regression">
              Regressão (Elite → High, etc)
            </mat-option>
            <mat-option value="tier_change">
              Qualquer mudança de tier
            </mat-option>
          </mat-select>
        </mat-form-field>

        <mat-form-field appearance="outline">
          <mat-label>Janela</mat-label>
          <mat-select [(ngModel)]="form.windowDays">
            <mat-option [value]="7">7 dias</mat-option>
            <mat-option [value]="30">30 dias</mat-option>
            <mat-option [value]="90">90 dias</mat-option>
          </mat-select>
        </mat-form-field>
      </div>

      <div class="row">
        <mat-form-field appearance="outline" class="grow">
          <mat-label>Escopo</mat-label>
          <mat-select [(ngModel)]="form.scopeKind">
            <mat-option value="project">Projeto</mat-option>
            <mat-option value="team">Time</mat-option>
            <mat-option value="tenant">Tenant inteiro</mat-option>
          </mat-select>
        </mat-form-field>

        <mat-form-field appearance="outline" class="grow">
          <mat-label>Scope ID (opcional)</mat-label>
          <input
            matInput
            [(ngModel)]="form.scopeId"
            placeholder="UUID — vazio = todos do tipo"
          />
        </mat-form-field>
      </div>

      <mat-form-field appearance="outline">
        <mat-label>Webhook URL</mat-label>
        <input
          matInput
          type="url"
          [(ngModel)]="form.webhookUrl"
          placeholder="https://hooks.slack.com/services/..."
        />
      </mat-form-field>

      <mat-slide-toggle [(ngModel)]="form.enabled">
        Regra ativa
      </mat-slide-toggle>

      @if (errorMsg(); as msg) {
        <div class="error">{{ msg }}</div>
      }
    </mat-dialog-content>

    <mat-dialog-actions align="end">
      <button mat-button (click)="cancel()" [disabled]="saving()">
        Cancelar
      </button>
      <button
        mat-flat-button
        color="primary"
        (click)="save()"
        [disabled]="saving() || !isValid()"
      >
        @if (saving()) {
          <mat-progress-spinner mode="indeterminate" diameter="18" />
        } @else {
          <mat-icon>save</mat-icon>
        }
        {{ data.rule ? 'Salvar' : 'Criar' }}
      </button>
    </mat-dialog-actions>
  `,
  styles: [
    `
      .content {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        min-width: 460px;
      }
      .title-icon {
        vertical-align: middle;
        margin-right: 8px;
      }
      .row {
        display: flex;
        gap: var(--space-3);
      }
      .row .grow {
        flex: 1;
      }
      .muted {
        color: var(--color-text-muted);
        margin: 0 0 var(--space-2);
      }
      .error {
        color: var(--color-danger);
        background: color-mix(in srgb, var(--color-danger) 10%, transparent);
        padding: var(--space-2) var(--space-3);
        border-radius: var(--radius-sm);
        font-size: 0.875rem;
      }
    `,
  ],
})
export class AlertRuleDialogComponent {
  data = inject<AlertRuleDialogData>(MAT_DIALOG_DATA);
  private dialogRef = inject(MatDialogRef<AlertRuleDialogComponent>);
  private api = inject(ApiClient);

  saving = signal(false);
  errorMsg = signal<string | null>(null);

  form: {
    name: string;
    enabled: boolean;
    kind: AlertKind;
    scopeKind: AlertScopeKind;
    scopeId: string;
    windowDays: 7 | 30 | 90;
    webhookUrl: string;
  } = {
    name: this.data.rule?.name ?? '',
    enabled: this.data.rule?.enabled ?? true,
    kind: this.data.rule?.kind ?? 'tier_regression',
    scopeKind: this.data.rule?.scopeKind ?? 'project',
    scopeId: this.data.rule?.scopeId ?? '',
    windowDays: this.data.rule?.windowDays ?? 30,
    webhookUrl: this.data.rule?.webhookUrl ?? '',
  };

  isValid(): boolean {
    return (
      this.form.name.trim().length > 0 &&
      this.form.webhookUrl.trim().length > 0
    );
  }

  cancel(): void {
    this.dialogRef.close(null);
  }

  save(): void {
    if (!this.isValid()) return;
    this.saving.set(true);
    this.errorMsg.set(null);

    const scopeId = this.form.scopeId.trim() || null;

    if (this.data.rule) {
      this.api
        .updateAlertRule(this.data.rule.id, {
          name: this.form.name.trim(),
          enabled: this.form.enabled,
          kind: this.form.kind,
          scopeKind: this.form.scopeKind,
          scopeId,
          windowDays: this.form.windowDays,
          webhookUrl: this.form.webhookUrl.trim(),
        })
        .subscribe({
          next: (rule) => this.dialogRef.close(rule),
          error: (err) => {
            this.saving.set(false);
            this.errorMsg.set(
              err?.error ?? err?.message ?? 'Erro ao atualizar regra',
            );
          },
        });
      return;
    }

    const body: CreateAlertRuleRequest = {
      tenant: this.data.tenant,
      name: this.form.name.trim(),
      enabled: this.form.enabled,
      kind: this.form.kind,
      scopeKind: this.form.scopeKind,
      scopeId,
      windowDays: this.form.windowDays,
      webhookUrl: this.form.webhookUrl.trim(),
    };
    this.api.createAlertRule(body).subscribe({
      next: (rule) => this.dialogRef.close(rule),
      error: (err) => {
        this.saving.set(false);
        this.errorMsg.set(
          err?.error ?? err?.message ?? 'Erro ao criar regra',
        );
      },
    });
  }
}
