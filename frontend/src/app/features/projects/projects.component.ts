import { ChangeDetectionStrategy, Component } from '@angular/core';
import { MatCardModule } from '@angular/material/card';

@Component({
  selector: 'app-projects',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MatCardModule],
  template: `
    <h1>Projetos</h1>
    <mat-card appearance="outlined">
      <mat-card-content>
        Nenhum projeto cadastrado. (Fase 1 — cadastro manual via API/CLI.)
      </mat-card-content>
    </mat-card>
  `,
})
export class ProjectsComponent {}
