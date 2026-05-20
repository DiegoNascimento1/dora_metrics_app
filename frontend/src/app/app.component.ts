import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { MatToolbarModule } from '@angular/material/toolbar';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';

@Component({
  selector: 'app-root',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    RouterOutlet,
    RouterLink,
    RouterLinkActive,
    MatToolbarModule,
    MatButtonModule,
    MatIconModule,
  ],
  template: `
    <mat-toolbar color="primary">
      <span>DORA Metrics</span>
      <span class="spacer"></span>
      <a mat-button routerLink="/dashboard" routerLinkActive="active">Dashboard</a>
      <a mat-button routerLink="/projects" routerLinkActive="active">Projetos</a>
      <a mat-button routerLink="/people" routerLinkActive="active">Pessoas</a>
    </mat-toolbar>

    <main class="container">
      <router-outlet />
    </main>
  `,
  styles: [
    `
      .spacer {
        flex: 1 1 auto;
      }
      .active {
        text-decoration: underline;
      }
    `,
  ],
})
export class AppComponent {}
