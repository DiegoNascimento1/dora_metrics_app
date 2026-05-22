import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  signal,
} from '@angular/core';
import { HttpErrorResponse } from '@angular/common/http';
import { MatCardModule } from '@angular/material/card';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatChipsModule } from '@angular/material/chips';
import { MatSnackBar } from '@angular/material/snack-bar';
import { catchError, of, finalize } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import { WeeklyDigest } from '../../core/api/api.types';
import { SkeletonComponent } from '../../shared/skeleton.component';

/**
 * Card semanal compartilhável: deploys/incidents da semana, delta de tier
 * vs semana anterior, top 3 contributors. Botão "Copiar como texto" gera
 * markdown pronto para colar em release notes / Slack.
 *
 * Inputs: scopeKind ('project'|'team') + scopeId. Faz fetch direto.
 */
@Component({
  selector: 'app-weekly-digest-card',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    MatCardModule,
    MatButtonModule,
    MatIconModule,
    MatChipsModule,
    SkeletonComponent,
  ],
  template: `
    <mat-card appearance="outlined" class="digest-card">
      <mat-card-header>
        <mat-card-title>
          <mat-icon fontIcon="newspaper" class="head-icon"></mat-icon>
          Resumo semanal
        </mat-card-title>
        @if (data(); as d) {
          <mat-card-subtitle>{{ d.isoWeek }} · {{ d.weekStart }} → {{ d.weekEnd }}</mat-card-subtitle>
        }
      </mat-card-header>

      <mat-card-content>
        @if (loading()) {
          <app-skeleton variant="text" width="70%" />
          <app-skeleton variant="text" width="40%" />
          <app-skeleton variant="text" width="60%" />
        } @else if (notFound()) {
          <p class="muted">
            Nenhum digest semanal ainda — a task <code>digest:weekly</code> roda
            toda segunda às 09:00 UTC. Em desenvolvimento, dispare manualmente
            via CLI.
          </p>
        } @else if (errorMsg()) {
          <p class="error">{{ errorMsg() }}</p>
        } @else if (data(); as d) {
          <div class="kpis">
            <div class="kpi">
              <span class="num">{{ d.deploymentsCount }}</span>
              <span class="label">deploys</span>
            </div>
            <div class="kpi">
              <span class="num">{{ d.incidentsCount }}</span>
              <span class="label">incidents</span>
            </div>
            <div class="kpi tier">
              @if (d.currentTier) {
                <mat-chip [class]="'tier-' + d.currentTier">{{ d.currentTier }}</mat-chip>
              } @else {
                <mat-chip class="tier-insufficient_data">sem dados</mat-chip>
              }
              @if (d.tierDelta !== 0) {
                <span [class.up]="d.tierDelta > 0" [class.down]="d.tierDelta < 0" class="delta">
                  <mat-icon [fontIcon]="d.tierDelta > 0 ? 'trending_up' : 'trending_down'"></mat-icon>
                  {{ d.tierDelta > 0 ? '+' : '' }}{{ d.tierDelta }} tier
                </span>
              }
            </div>
          </div>

          @if (d.topContributors.length > 0) {
            <div class="top">
              <strong>Top contributors</strong>
              <ul>
                @for (c of d.topContributors; track c.name) {
                  <li>
                    <span class="rank">#{{ $index + 1 }}</span>
                    <span class="name">{{ c.name }}</span>
                    <span class="muted">{{ c.deploys }} deploy{{ c.deploys === 1 ? '' : 's' }}</span>
                  </li>
                }
              </ul>
            </div>
          }
        }
      </mat-card-content>

      @if (data() && !loading()) {
        <mat-card-actions align="end">
          <button mat-button (click)="copyAsMarkdown()">
            <mat-icon fontIcon="content_copy"></mat-icon>
            Copiar como markdown
          </button>
        </mat-card-actions>
      }
    </mat-card>
  `,
  styles: [
    `
      :host {
        display: block;
        margin-top: var(--space-3);
      }
      .digest-card mat-card-title {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
      }
      .head-icon {
        color: var(--color-brand);
      }
      .kpis {
        display: flex;
        gap: var(--space-4);
        margin: var(--space-3) 0;
        flex-wrap: wrap;
      }
      .kpi {
        display: flex;
        flex-direction: column;
        align-items: flex-start;
      }
      .kpi.tier {
        flex-direction: row;
        align-items: center;
        gap: var(--space-2);
      }
      .num {
        font-size: 1.75rem;
        font-weight: 600;
        font-variant-numeric: tabular-nums;
        line-height: 1;
      }
      .label {
        color: var(--color-text-secondary);
        font-size: var(--font-size-sm);
      }
      .delta {
        display: inline-flex;
        align-items: center;
        gap: 2px;
        font-size: var(--font-size-sm);
        font-weight: 600;
      }
      .delta.up {
        color: var(--color-tier-elite);
      }
      .delta.down {
        color: var(--color-tier-low);
      }
      .delta mat-icon {
        font-size: 16px;
        height: 16px;
        width: 16px;
      }
      .top {
        margin-top: var(--space-3);
      }
      .top ul {
        margin: var(--space-2) 0 0;
        padding: 0;
        list-style: none;
      }
      .top li {
        display: flex;
        gap: var(--space-2);
        padding: var(--space-1) 0;
        align-items: center;
      }
      .rank {
        font-weight: 700;
        color: var(--color-text-muted);
        min-width: 28px;
      }
      .name {
        flex: 1;
        font-weight: 500;
      }
      .muted {
        color: var(--color-text-muted);
      }
      .error {
        color: var(--color-tier-low);
      }
      code {
        font-family: var(--font-mono);
        font-size: 0.85em;
        background: var(--color-surface-subtle);
        padding: 1px 6px;
        border-radius: var(--radius-sm);
      }
    `,
  ],
})
export class WeeklyDigestCardComponent {
  private api = inject(ApiClient);
  private snack = inject(MatSnackBar);

  scopeKind = input.required<'project' | 'team'>();
  scopeId = input<string | null>(null);

  loading = signal(false);
  notFound = signal(false);
  errorMsg = signal<string | null>(null);
  data = signal<WeeklyDigest | null>(null);

  protected hasData = computed(() => this.data() !== null);

  constructor() {
    effect(() => {
      const id = this.scopeId();
      if (!id) {
        this.data.set(null);
        return;
      }
      this.load(this.scopeKind(), id);
    });
  }

  private load(kind: 'project' | 'team', id: string): void {
    this.loading.set(true);
    this.notFound.set(false);
    this.errorMsg.set(null);

    const obs = kind === 'team'
      ? this.api.getTeamDigest(id)
      : this.api.getProjectDigest(id);

    obs
      .pipe(
        catchError((err: HttpErrorResponse) => {
          if (err.status === 404) {
            this.notFound.set(true);
          } else {
            this.errorMsg.set(err.message ?? 'Erro ao carregar digest.');
          }
          return of(null);
        }),
        finalize(() => this.loading.set(false)),
      )
      .subscribe((d) => this.data.set(d));
  }

  copyAsMarkdown(): void {
    const d = this.data();
    if (!d) return;
    const lines: string[] = [];
    lines.push(`# Resumo semanal — ${d.isoWeek}`);
    lines.push('');
    lines.push(`📅 ${d.weekStart} → ${d.weekEnd}`);
    lines.push('');
    lines.push(`- 🚀 **${d.deploymentsCount}** deploys`);
    lines.push(`- 🔥 **${d.incidentsCount}** incidents`);
    if (d.currentTier) {
      let line = `- 🏷️ Tier: **${d.currentTier}**`;
      if (d.tierDelta !== 0) {
        line += d.tierDelta > 0 ? ` (subiu ${d.tierDelta})` : ` (caiu ${Math.abs(d.tierDelta)})`;
      }
      lines.push(line);
    }
    if (d.topContributors.length > 0) {
      lines.push('');
      lines.push('## Top contributors');
      d.topContributors.forEach((c, i) => {
        lines.push(`${i + 1}. ${c.name} — ${c.deploys} deploys`);
      });
    }
    const text = lines.join('\n');
    navigator.clipboard
      .writeText(text)
      .then(() => this.snack.open('Digest copiado pra área de transferência', '', { duration: 2500 }))
      .catch(() => this.snack.open('Não foi possível copiar — verifique permissões.', 'OK', { duration: 4000 }));
  }
}
