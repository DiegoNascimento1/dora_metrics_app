import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { catchError, of, finalize } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import { Project } from '../../core/api/api.types';

@Component({
  selector: 'app-projects',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MatCardModule, MatTableModule, MatProgressSpinnerModule],
  template: `
    <h1>Projetos</h1>

    @if (loading()) {
      <mat-progress-spinner mode="indeterminate" diameter="40" />
    } @else if (projects().length === 0) {
      <mat-card appearance="outlined">
        <mat-card-content>
          Nenhum projeto cadastrado. Use a CLI: <code>cli project add</code>.
        </mat-card-content>
      </mat-card>
    } @else {
      <table mat-table [dataSource]="projects()">
        <ng-container matColumnDef="path">
          <th mat-header-cell *matHeaderCellDef>Path</th>
          <td mat-cell *matCellDef="let p">{{ p.pathWithNamespace }}</td>
        </ng-container>
        <ng-container matColumnDef="id">
          <th mat-header-cell *matHeaderCellDef>ID</th>
          <td mat-cell *matCellDef="let p"><code>{{ p.id }}</code></td>
        </ng-container>
        <ng-container matColumnDef="active">
          <th mat-header-cell *matHeaderCellDef>Status</th>
          <td mat-cell *matCellDef="let p">{{ p.active ? 'ativo' : 'inativo' }}</td>
        </ng-container>

        <tr mat-header-row *matHeaderRowDef="cols"></tr>
        <tr mat-row *matRowDef="let row; columns: cols"></tr>
      </table>
    }
  `,
  styles: [
    `
      table {
        width: 100%;
        margin-top: 16px;
      }
      code {
        font-size: 0.875rem;
        color: #555;
      }
    `,
  ],
})
export class ProjectsComponent {
  private api = inject(ApiClient);

  loading = signal(false);
  projects = signal<Project[]>([]);
  cols = ['path', 'id', 'active'];

  constructor() {
    this.loading.set(true);
    this.api
      .listProjects()
      .pipe(
        catchError(() => of([] as Project[])),
        finalize(() => this.loading.set(false)),
      )
      .subscribe((p) => this.projects.set(p));
  }
}
