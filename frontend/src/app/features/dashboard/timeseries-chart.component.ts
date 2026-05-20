import {
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
} from '@angular/core';
import { BaseChartDirective } from 'ng2-charts';
import {
  ChartConfiguration,
  ChartData,
  Chart,
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend,
} from 'chart.js';

import { TimeseriesPoint } from '../../core/api/api.types';

Chart.register(CategoryScale, LinearScale, BarElement, Title, Tooltip, Legend);

@Component({
  selector: 'app-timeseries-chart',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [BaseChartDirective],
  template: `
    <canvas
      baseChart
      [type]="chartType"
      [data]="chartData()"
      [options]="chartOptions"
    >
    </canvas>
  `,
  styles: [
    `
      :host {
        display: block;
        height: 240px;
        position: relative;
      }
      canvas {
        max-height: 240px;
      }
    `,
  ],
})
export class TimeseriesChartComponent {
  points = input<TimeseriesPoint[]>([]);

  readonly chartType = 'bar' as const;

  chartData = computed<ChartData<'bar'>>(() => {
    const data = this.points();
    return {
      labels: data.map((p) => p.day.substring(5)), // MM-DD para economizar espaço
      datasets: [
        {
          label: 'Deploys',
          data: data.map((p) => p.deployCount),
          backgroundColor: '#1976d2',
          borderRadius: 2,
        },
      ],
    };
  });

  chartOptions: ChartConfiguration<'bar'>['options'] = {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: { display: false },
      tooltip: { intersect: false, mode: 'index' },
    },
    scales: {
      y: {
        beginAtZero: true,
        ticks: { stepSize: 1, precision: 0 },
      },
      x: {
        ticks: { autoSkip: true, maxRotation: 0 },
      },
    },
  };
}
