import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
import { MatChipsModule } from '@angular/material/chips';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatTableModule } from '@angular/material/table';
import { FormsModule } from '@angular/forms';
import { forkJoin, of, catchError, finalize } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import {
  Classification,
  DoraMetrics,
  Project,
  Team,
  TimeseriesPoint,
} from '../../core/api/api.types';
import { SkeletonComponent } from '../../shared/skeleton.component';
import { EmptyStateComponent } from '../../shared/empty-state.component';
import { ErrorStateComponent } from '../../shared/error-state.component';
import { TimeseriesChartComponent } from '../dashboard/timeseries-chart.component';
import {
  MetricKey,
  TIER_RANK,
  formatMetricValue,
} from '../../shared/dora-tiers';

type CompareScope = 'project' | 'team';
type Window = '7d' | '30d' | '90d';

interface Selectable {
  id: string;
  label: string;
}

interface CompareResult {
  id: string;
  label: string;
  metrics: DoraMetrics | null;
  points: TimeseriesPoint[];
}

interface MetricRow {
  key: MetricKey;
  label: string;
  cells: { id: string; value: string; tier: Classification; isBest: boolean }[];
}

/**
 * Compare mode: 2–4 escopos (times OU projetos) lado-a-lado, com gráficos
 * sobrepostos das séries de deploys + tabela das 4 métricas DORA com
 * destaque do melhor valor por linha.
 */
@Component({
  selector: 'app-compare',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    MatCardModule,
    MatFormFieldModule,
    MatSelectModule,
    MatChipsModule,
    MatButtonModule,
    MatIconModule,
    MatTableModule,
    SkeletonComponent,
    EmptyStateComponent,
    ErrorStateComponent,
    TimeseriesChartComponent,
  ],
  template: `
    <h1>Comparar — lado a lado</h1>

    <p class="lede">
      Escolha 2 a 4 {{ scope === 'team' ? 'times' : 'projetos' }} para sobrepor
      séries de deploys e comparar as 4 métricas DORA na mesma janela.
    </p>

    <div class="filters">
      <mat-form-field appearance="outline">
        <mat-label>Escopo</mat-label>
        <mat-select [(value)]="scope" (selectionChange)="onScopeChange()">
          <mat-option value="project">Projetos</mat-option>
          <mat-option value="team">Times</mat-option>
        </mat-select>
      </mat-form-field>

      <mat-form-field appearance="outline">
        <mat-label>Janela</mat-label>
        <mat-select [(value)]="window" (selectionChange)="loadAll()">
          <mat-option value="7d">7 dias</mat-option>
          <mat-option value="30d">30 dias</mat-option>
          <mat-option value="90d">90 dias</mat-option>
        </mat-select>
      </mat-form-field>

      <mat-form-field appearance="outline" class="multi">
        <mat-label>Selecione 2–4 {{ scope === 'team' ? 'times' : 'projetos' }}</mat-label>
        <mat-select
          multiple
          [(value)]="selectedIds"
          (selectionChange)="onSelectionChange($event.value)"
        >
          @for (o of options(); track o.id) {
            <mat-option
              [value]="o.id"
              [disabled]="selectedIds.length >= 4 && !selectedIds.includes(o.id)"
            >
              {{ o.label }}
            </mat-option>
          }
        </mat-select>
      </mat-form-field>

      <button mat-stroked-button (click)="loadAll()">
        <mat-icon fontIcon="refresh"></mat-icon>
        Atualizar
      </button>
    </div>

    @if (loading()) {
      <app-skeleton variant="card" height="240px" />
    } @else if (error()) {
      <mat-card appearance="outlined">
        <mat-card-content>
          <app-error-state
            variant="network"
            title="Falha ao carregar comparativo"
            [description]="error() || ''"
          >
            <button mat-flat-button color="primary" (click)="loadAll()">
              Tentar novamente
            </button>
          </app-error-state>
        </mat-card-content>
      </mat-card>
    } @else if (selectedIds.length < 2) {
      <mat-card appearance="outlined">
        <mat-card-content>
          <app-empty-state
            icon="compare_arrows"
            title="Selecione ao menos 2"
            description="Escolha 2, 3 ou 4 itens no seletor acima para ver o comparativo."
          />
        </mat-card-content>
      </mat-card>
    } @else {
      <mat-card appearance="outlined" class="chart-card">
        <mat-card-header>
          <mat-card-title>Deploys/dia — séries sobrepostas</mat-card-title>
        </mat-card-header>
        <mat-card-content>
          @if (overlaidPoints().length === 0) {
            <p class="empty">Sem deploys na janela.</p>
          } @else {
            <app-timeseries-chart [points]="overlaidPoints()" />
          }
        </mat-card-content>
      </mat-card>

      <mat-card appearance="outlined" class="table-card">
        <mat-card-header>
          <mat-card-title>Métricas DORA — melhor por linha em verde</mat-card-title>
        </mat-card-header>
        <mat-card-content>
          <table mat-table [dataSource]="rows()" class="cmp-table">
            <ng-container matColumnDef="label">
              <th mat-header-cell *matHeaderCellDef>Métrica</th>
              <td mat-cell *matCellDef="let r">{{ r.label }}</td>
            </ng-container>
            @for (col of selectedIds; track col) {
              <ng-container [matColumnDef]="col">
                <th mat-header-cell *matHeaderCellDef>
                  {{ labelById(col) }}
                </th>
                <td mat-cell *matCellDef="let r">
                  @for (c of r.cells; track c.id) {
                    @if (c.id === col) {
                      <span [class.best]="c.isBest">{{ c.value }}</span>
                      <mat-chip [class]="'tier-' + c.tier">{{ c.tier }}</mat-chip>
                    }
                  }
                </td>
              </ng-container>
            }
            <tr mat-header-row *matHeaderRowDef="displayedColumns()"></tr>
            <tr mat-row *matRowDef="let r; columns: displayedColumns()"></tr>
          </table>
        </mat-card-content>
      </mat-card>
    }
  `,
  styles: [
    `
      :host {
        display: block;
        padding: var(--space-4);
      }
      .lede {
        color: var(--color-text-secondary);
        max-width: 720px;
      }
      .filters {
        display: flex;
        gap: var(--space-3);
        flex-wrap: wrap;
        align-items: center;
        margin: var(--space-3) 0;
      }
      .multi {
        min-width: 320px;
      }
      .chart-card,
      .table-card {
        margin-top: var(--space-3);
      }
      .cmp-table {
        width: 100%;
      }
      .cmp-table .best {
        color: var(--color-tier-elite);
        font-weight: 700;
      }
      .cmp-table mat-chip {
        margin-left: var(--space-2);
      }
      .empty {
        color: var(--color-text-muted);
        margin: var(--space-3) 0;
      }
    `,
  ],
})
export class CompareComponent {
  private api = inject(ApiClient);

  loading = signal(false);
  error = signal<string | null>(null);
  results = signal<CompareResult[]>([]);
  private projects = signal<Project[]>([]);
  private teams = signal<Team[]>([]);

  /** bound via [(value)] do <mat-select>. */
  public scope: CompareScope = 'project';
  public window: Window = '30d';
  public selectedIds: string[] = [];

  options = computed<Selectable[]>(() => {
    if (this.scope === 'team') {
      return this.teams().map((t) => ({
        id: t.id,
        label: `${t.emoji || '👥'} ${t.name}`,
      }));
    }
    return this.projects().map((p) => ({ id: p.id, label: p.pathWithNamespace }));
  });

  displayedColumns = computed<string[]>(() => ['label', ...this.selectedIds]);

  overlaidPoints = computed<TimeseriesPoint[]>(() => {
    // Para "overlay simples": concatena as séries marcando ProjectId (re-uso
    // do TimeseriesChartComponent que já agrupa por bucket). Se o chart não
    // diferenciar series, pelo menos visualiza a soma — TODO: estender o
    // chart pra multi-series por id quando houver dataset real.
    return this.results().flatMap((r) => r.points);
  });

  rows = computed<MetricRow[]>(() => {
    const data = this.results();
    if (data.length === 0) return [];

    const buildRow = (key: MetricKey, label: string): MetricRow => {
      const cells = data.map((r) => {
        const m = r.metrics;
        const value = pick(m, key);
        const tier = m?.classification ?? 'insufficient_data';
        return {
          id: r.id,
          value: m ? formatMetricValue(key, value) : '—',
          tier,
          isBest: false,
        };
      });

      // Best por tier (mais alto). Empata? marca todos.
      const bestRank = Math.max(...cells.map((c) => TIER_RANK[c.tier]));
      if (bestRank > 0) {
        for (const c of cells) {
          if (TIER_RANK[c.tier] === bestRank) c.isBest = true;
        }
      }

      return { key, label, cells };
    };

    return [
      buildRow('df', 'Deployment Frequency'),
      buildRow('lt', 'Lead Time (mediana)'),
      buildRow('cfr', 'Change Failure Rate'),
      buildRow('mttr', 'MTTR (média)'),
    ];
  });

  constructor() {
    this.loadOptions();
  }

  private loadOptions(): void {
    this.loading.set(true);
    forkJoin({
      projects: this.api.listProjects().pipe(catchError(() => of([] as Project[]))),
      teams: this.api.listTeams('acme').pipe(catchError(() => of([] as Team[]))),
    })
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe(({ projects, teams }) => {
        this.projects.set(projects);
        this.teams.set(teams);
      });
  }

  onScopeChange(): void {
    this.selectedIds = [];
    this.results.set([]);
  }

  onSelectionChange(ids: string[]): void {
    this.selectedIds = ids.slice(0, 4);
    this.loadAll();
  }

  loadAll(): void {
    if (this.selectedIds.length < 2) {
      this.results.set([]);
      return;
    }
    this.loading.set(true);
    this.error.set(null);

    const calls = this.selectedIds.map((id) => {
      const label = this.labelById(id);
      if (this.scope === 'team') {
        return forkJoin({
          id: of(id),
          label: of(label),
          metrics: this.api
            .getTeamMetrics(id, this.window)
            .pipe(catchError(() => of<DoraMetrics | null>(null))),
          timeseries: this.api
            .getTeamTimeseries(id, this.window)
            .pipe(catchError(() => of({ points: [] as TimeseriesPoint[] }))),
        });
      }
      return forkJoin({
        id: of(id),
        label: of(label),
        metrics: this.api
          .getProjectMetrics(id, this.window)
          .pipe(catchError(() => of<DoraMetrics | null>(null))),
        timeseries: this.api
          .getProjectTimeseries(id, this.window)
          .pipe(catchError(() => of({ points: [] as TimeseriesPoint[] }))),
      });
    });

    forkJoin(calls)
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe({
        next: (rows) => {
          this.results.set(
            rows.map((r) => ({
              id: r.id,
              label: r.label,
              metrics: r.metrics,
              points: r.timeseries.points ?? [],
            })),
          );
        },
        error: (err) => {
          this.error.set(err instanceof Error ? err.message : 'Erro ao comparar.');
        },
      });
  }

  labelById(id: string): string {
    return this.options().find((o) => o.id === id)?.label ?? id;
  }
}

function pick(m: DoraMetrics | null, key: MetricKey): number | null {
  if (!m) return null;
  switch (key) {
    case 'df':
      return m.deploymentFrequency;
    case 'lt':
      return m.leadTimeMedianSeconds;
    case 'cfr':
      return m.changeFailureRate;
    case 'mttr':
      return m.mttrMeanSeconds;
  }
}
