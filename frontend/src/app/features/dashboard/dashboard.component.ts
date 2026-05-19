import { ChangeDetectionStrategy, Component, signal } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';

import { Classification, DoraMetrics } from '../../core/api/api.types';

interface MetricTile {
  label: string;
  value: string;
  classification: Classification;
}

@Component({
  selector: 'app-dashboard',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MatCardModule, MatChipsModule, MatProgressSpinnerModule],
  template: `
    <h1>DORA — visão geral</h1>

    @if (loading()) {
      <mat-progress-spinner mode="indeterminate" diameter="40" />
    } @else {
      <div class="grid">
        @for (tile of tiles(); track tile.label) {
          <mat-card appearance="outlined">
            <mat-card-header>
              <mat-card-title>{{ tile.label }}</mat-card-title>
            </mat-card-header>
            <mat-card-content>
              <div class="value">{{ tile.value }}</div>
              <mat-chip [class]="'tier-' + tile.classification">
                {{ tile.classification }}
              </mat-chip>
            </mat-card-content>
          </mat-card>
        }
      </div>
    }
  `,
  styles: [
    `
      .grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
        gap: 16px;
        margin-top: 24px;
      }
      .value {
        font-size: 2rem;
        font-weight: 500;
        margin: 12px 0;
      }
      .tier-elite { background: #2e7d32; color: white; }
      .tier-high { background: #1976d2; color: white; }
      .tier-medium { background: #f9a825; color: black; }
      .tier-low { background: #c62828; color: white; }
      .tier-insufficient_data { background: #757575; color: white; }
    `,
  ],
})
export class DashboardComponent {
  loading = signal(false);

  // TODO Fase 1: consumir ApiClient.getProjectMetrics() de um projeto selecionado.
  private placeholderMetrics: DoraMetrics = {
    projectId: '00000000-0000-0000-0000-000000000000',
    windowDays: 30,
    computedAt: new Date().toISOString(),
    deploymentFrequency: 0,
    leadTimeMedianSeconds: null,
    changeFailureRate: null,
    mttrMeanSeconds: null,
    classification: 'insufficient_data',
    sampleSize: 0,
  };

  tiles = signal<MetricTile[]>([
    {
      label: 'Deployment Frequency',
      value: `${this.placeholderMetrics.deploymentFrequency.toFixed(2)}/dia`,
      classification: this.placeholderMetrics.classification,
    },
    {
      label: 'Lead Time (mediana)',
      value: this.formatDuration(this.placeholderMetrics.leadTimeMedianSeconds),
      classification: this.placeholderMetrics.classification,
    },
    {
      label: 'Change Failure Rate',
      value:
        this.placeholderMetrics.changeFailureRate === null
          ? '—'
          : `${(this.placeholderMetrics.changeFailureRate * 100).toFixed(1)}%`,
      classification: this.placeholderMetrics.classification,
    },
    {
      label: 'MTTR (média)',
      value: this.formatDuration(this.placeholderMetrics.mttrMeanSeconds),
      classification: this.placeholderMetrics.classification,
    },
  ]);

  private formatDuration(seconds: number | null): string {
    if (seconds === null) return '—';
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${(seconds / 60).toFixed(0)}min`;
    if (seconds < 86400) return `${(seconds / 3600).toFixed(1)}h`;
    return `${(seconds / 86400).toFixed(1)}d`;
  }
}
