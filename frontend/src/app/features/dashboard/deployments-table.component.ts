import {
  ChangeDetectionStrategy,
  Component,
  input,
} from '@angular/core';
import { DatePipe } from '@angular/common';
import { MatTableModule } from '@angular/material/table';

import { Deployment } from '../../core/api/api.types';

@Component({
  selector: 'app-deployments-table',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, MatTableModule],
  template: `
    <table mat-table [dataSource]="deployments()">
      <ng-container matColumnDef="finishedAt">
        <th mat-header-cell *matHeaderCellDef>Finalizado</th>
        <td mat-cell *matCellDef="let d">
          {{ d.finishedAt | date: 'short' }}
        </td>
      </ng-container>

      <ng-container matColumnDef="sha">
        <th mat-header-cell *matHeaderCellDef>SHA</th>
        <td mat-cell *matCellDef="let d">
          <code>{{ d.sha.substring(0, 8) }}</code>
        </td>
      </ng-container>

      <ng-container matColumnDef="ref">
        <th mat-header-cell *matHeaderCellDef>Ref</th>
        <td mat-cell *matCellDef="let d">{{ d.ref ?? '—' }}</td>
      </ng-container>

      <ng-container matColumnDef="env">
        <th mat-header-cell *matHeaderCellDef>Ambiente</th>
        <td mat-cell *matCellDef="let d">{{ d.environmentName }}</td>
      </ng-container>

      <ng-container matColumnDef="trigger">
        <th mat-header-cell *matHeaderCellDef>Por</th>
        <td mat-cell *matCellDef="let d">{{ d.triggeredBy ?? '—' }}</td>
      </ng-container>

      <tr mat-header-row *matHeaderRowDef="cols"></tr>
      <tr mat-row *matRowDef="let row; columns: cols"></tr>
    </table>
  `,
  styles: [
    `
      table {
        width: 100%;
      }
      code {
        font-family: monospace;
        font-size: 0.875rem;
        color: #555;
      }
    `,
  ],
})
export class DeploymentsTableComponent {
  deployments = input<Deployment[]>([]);
  cols = ['finishedAt', 'sha', 'ref', 'env', 'trigger'];
}
