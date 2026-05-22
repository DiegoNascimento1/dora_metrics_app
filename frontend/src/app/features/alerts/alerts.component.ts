import {
  ChangeDetectionStrategy,
  Component,
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
import { MatTooltipModule } from '@angular/material/tooltip';
import { MatChipsModule } from '@angular/material/chips';
import { forkJoin, of } from 'rxjs';
import { catchError, finalize } from 'rxjs/operators';

import { ApiClient } from '../../core/api/api.client';
import { AlertEvent, AlertRule } from '../../core/api/api.types';
import { SkeletonComponent } from '../../shared/skeleton.component';
import { EmptyStateComponent } from '../../shared/empty-state.component';
import {
  AlertRuleDialogComponent,
  AlertRuleDialogData,
} from './alert-rule-dialog.component';

@Component({
  selector: 'app-alerts',
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
    MatTooltipModule,
    MatChipsModule,
    SkeletonComponent,
    EmptyStateComponent,
  ],
  template: `
    <h1>Alertas</h1>

    <div class="filters">
      <mat-form-field appearance="outline">
        <mat-label>Tenant</mat-label>
        <input
          matInput
          [(ngModel)]="tenant"
          (change)="reload()"
          placeholder="acme"
        />
      </mat-form-field>
      <button mat-stroked-button (click)="reload()">
        <mat-icon fontIcon="refresh"></mat-icon>
        Atualizar
      </button>
      <button mat-flat-button color="primary" (click)="openCreate()">
        <mat-icon fontIcon="add"></mat-icon>
        Nova regra
      </button>
    </div>

    <mat-card appearance="outlined" class="section-card">
      <mat-card-header>
        <mat-card-title>Regras</mat-card-title>
        <mat-card-subtitle>
          Webhooks disparados em mudança de classificação DORA combinada
        </mat-card-subtitle>
      </mat-card-header>
      <mat-card-content>
        @if (loadingRules()) {
          <div class="skeleton-list">
            @for (_ of [0, 1, 2]; track $index) {
              <app-skeleton variant="card" height="80px" />
            }
          </div>
        } @else if (rules().length === 0) {
          <app-empty-state
            icon="notifications_off"
            title="Nenhuma regra criada"
            description="Crie sua primeira regra de alerta para receber webhook quando a classificação DORA mudar."
          />
        } @else {
          <div class="rules-list">
            @for (rule of rules(); track rule.id) {
              <div class="rule-row">
                <div class="rule-main">
                  <div class="rule-head">
                    <strong>{{ rule.name }}</strong>
                    <mat-chip [class.chip-on]="rule.enabled" [class.chip-off]="!rule.enabled">
                      {{ rule.enabled ? 'Ativa' : 'Pausada' }}
                    </mat-chip>
                    <mat-chip class="chip-kind">
                      {{ rule.kind === 'tier_regression' ? 'Regressão' : 'Qualquer mudança' }}
                    </mat-chip>
                    <mat-chip class="chip-scope">
                      {{ rule.scopeKind }} · {{ rule.windowDays }}d
                    </mat-chip>
                  </div>
                  <div class="rule-meta">
                    <mat-icon class="row-icon" fontIcon="link"></mat-icon>
                    <code>{{ rule.webhookUrl }}</code>
                  </div>
                  <div class="rule-meta-2">
                    criado {{ rule.createdAt | date: 'short' }} ·
                    atualizado {{ rule.updatedAt | date: 'short' }}
                  </div>
                </div>
                <div class="rule-actions">
                  <button
                    mat-icon-button
                    matTooltip="Editar"
                    (click)="openEdit(rule)"
                  >
                    <mat-icon fontIcon="edit"></mat-icon>
                  </button>
                  <button
                    mat-icon-button
                    color="warn"
                    matTooltip="Remover"
                    (click)="remove(rule)"
                  >
                    <mat-icon fontIcon="delete_outline"></mat-icon>
                  </button>
                </div>
              </div>
            }
          </div>
        }
      </mat-card-content>
    </mat-card>

    <mat-card appearance="outlined" class="section-card">
      <mat-card-header>
        <mat-card-title>Histórico de disparos</mat-card-title>
        <mat-card-subtitle>
          Últimos {{ events().length }} eventos — status de entrega rastreado para retry
        </mat-card-subtitle>
      </mat-card-header>
      <mat-card-content>
        @if (loadingEvents()) {
          <div class="skeleton-list">
            @for (_ of [0, 1, 2]; track $index) {
              <app-skeleton variant="card" height="56px" />
            }
          </div>
        } @else if (events().length === 0) {
          <app-empty-state
            icon="history"
            title="Nenhum disparo ainda"
            description="Quando a classificação DORA combinada mudar, os eventos aparecem aqui."
          />
        } @else {
          <table class="events-table">
            <thead>
              <tr>
                <th>Quando</th>
                <th>Transição</th>
                <th>Escopo</th>
                <th>Status</th>
                <th>HTTP</th>
                <th>Detalhe</th>
              </tr>
            </thead>
            <tbody>
              @for (e of events(); track e.id) {
                <tr>
                  <td>{{ e.firedAt | date: 'short' }}</td>
                  <td>
                    <code>{{ e.previousTier ?? '—' }}</code>
                    <mat-icon class="arrow" fontIcon="arrow_right_alt"></mat-icon>
                    <code>{{ e.currentTier }}</code>
                  </td>
                  <td>{{ e.scopeKind }}</td>
                  <td>
                    <span class="status status-{{ e.deliveryStatus }}">
                      {{ statusLabel(e.deliveryStatus) }}
                    </span>
                  </td>
                  <td>{{ e.httpStatus ?? '—' }}</td>
                  <td class="error-cell">
                    @if (e.lastError) {
                      <span [matTooltip]="e.lastError">{{ e.lastError }}</span>
                    } @else if (e.deliveredAt) {
                      entregue {{ e.deliveredAt | date: 'short' }}
                    } @else {
                      —
                    }
                  </td>
                </tr>
              }
            </tbody>
          </table>
        }
      </mat-card-content>
    </mat-card>
  `,
  styles: [
    `
      .filters {
        display: flex;
        align-items: center;
        gap: var(--space-4);
        margin: var(--space-4) 0;
      }
      .section-card {
        margin-bottom: var(--space-4);
      }
      .skeleton-list {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .rules-list {
        display: flex;
        flex-direction: column;
      }
      .rule-row {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        gap: var(--space-3);
        padding: var(--space-3) 0;
        border-bottom: 1px solid var(--color-border);
      }
      .rule-row:last-child {
        border-bottom: none;
      }
      .rule-main {
        display: flex;
        flex-direction: column;
        gap: 4px;
        min-width: 0;
        flex: 1;
      }
      .rule-head {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        flex-wrap: wrap;
      }
      .rule-meta {
        display: flex;
        align-items: center;
        gap: 6px;
        color: var(--color-text-muted);
        font-size: 0.8125rem;
        min-width: 0;
      }
      .rule-meta code {
        background: var(--color-surface-subtle);
        padding: 2px 6px;
        border-radius: var(--radius-sm);
        font-family: var(--font-mono);
        font-size: 0.85em;
        max-width: 100%;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
      .rule-meta-2 {
        color: var(--color-text-muted);
        font-size: 0.75rem;
      }
      .row-icon {
        font-size: 16px;
        height: 16px;
        width: 16px;
        opacity: 0.7;
      }
      .chip-on {
        background: color-mix(in srgb, var(--status-elite) 18%, transparent);
        color: var(--status-elite);
      }
      .chip-off {
        background: var(--color-surface-subtle);
        color: var(--color-text-muted);
      }
      .chip-kind,
      .chip-scope {
        background: var(--color-surface-subtle);
        color: var(--color-text);
      }
      .events-table {
        width: 100%;
        border-collapse: collapse;
        font-size: 0.875rem;
      }
      .events-table th,
      .events-table td {
        text-align: left;
        padding: var(--space-2) var(--space-3);
        border-bottom: 1px solid var(--color-border);
      }
      .events-table th {
        font-weight: 600;
        color: var(--color-text-muted);
        font-size: 0.75rem;
        text-transform: uppercase;
        letter-spacing: 0.04em;
      }
      .events-table code {
        background: var(--color-surface-subtle);
        padding: 2px 6px;
        border-radius: var(--radius-sm);
        font-family: var(--font-mono);
        font-size: 0.85em;
      }
      .arrow {
        vertical-align: middle;
        font-size: 18px;
        height: 18px;
        width: 18px;
        margin: 0 4px;
        opacity: 0.6;
      }
      .status {
        font-weight: 600;
        font-size: 0.75rem;
        padding: 2px 10px;
        border-radius: 999px;
      }
      .status-delivered {
        background: color-mix(in srgb, var(--status-elite) 18%, transparent);
        color: var(--status-elite);
      }
      .status-pending {
        background: color-mix(in srgb, var(--status-high) 18%, transparent);
        color: var(--status-high);
      }
      .status-failed {
        background: color-mix(in srgb, var(--color-danger) 18%, transparent);
        color: var(--color-danger);
      }
      .error-cell {
        max-width: 320px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
    `,
  ],
})
export class AlertsComponent {
  private api = inject(ApiClient);
  private dialog = inject(MatDialog);
  private snack = inject(MatSnackBar);

  tenant = 'acme';
  loadingRules = signal(false);
  loadingEvents = signal(false);
  rules = signal<AlertRule[]>([]);
  events = signal<AlertEvent[]>([]);

  constructor() {
    this.reload();
  }

  reload(): void {
    if (!this.tenant) return;
    this.loadingRules.set(true);
    this.loadingEvents.set(true);
    forkJoin({
      rules: this.api
        .listAlertRules(this.tenant)
        .pipe(catchError(() => of([] as AlertRule[]))),
      events: this.api
        .listAlertEvents(this.tenant, 50)
        .pipe(catchError(() => of([] as AlertEvent[]))),
    })
      .pipe(
        finalize(() => {
          this.loadingRules.set(false);
          this.loadingEvents.set(false);
        }),
      )
      .subscribe(({ rules, events }) => {
        this.rules.set(rules);
        this.events.set(events);
      });
  }

  openCreate(): void {
    const data: AlertRuleDialogData = { tenant: this.tenant };
    const ref = this.dialog.open(AlertRuleDialogComponent, {
      data,
      width: 'min(600px, 92vw)',
      autoFocus: 'first-tabbable',
    });
    ref.afterClosed().subscribe((rule: AlertRule | null) => {
      if (rule) {
        this.snack.open(`Regra criada: ${rule.name}`, 'OK', { duration: 3500 });
        this.reload();
      }
    });
  }

  openEdit(rule: AlertRule): void {
    const data: AlertRuleDialogData = { tenant: this.tenant, rule };
    const ref = this.dialog.open(AlertRuleDialogComponent, {
      data,
      width: 'min(600px, 92vw)',
      autoFocus: 'first-tabbable',
    });
    ref.afterClosed().subscribe((updated: AlertRule | null) => {
      if (updated) {
        this.snack.open(`Regra atualizada: ${updated.name}`, 'OK', {
          duration: 3500,
        });
        this.reload();
      }
    });
  }

  remove(rule: AlertRule): void {
    if (!confirm(`Remover a regra "${rule.name}"?`)) return;
    this.api.deleteAlertRule(rule.id).subscribe({
      next: () => {
        this.snack.open(`Regra ${rule.name} removida`, 'OK', {
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

  statusLabel(status: AlertEvent['deliveryStatus']): string {
    switch (status) {
      case 'delivered':
        return 'Entregue';
      case 'failed':
        return 'Falhou';
      default:
        return 'Pendente';
    }
  }
}
