import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatCardModule } from '@angular/material/card';
import { MatTableModule } from '@angular/material/table';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { forkJoin, of, catchError, finalize } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import {
  Identity,
  MergeSuggestion,
  PersonWithIdentities,
} from '../../core/api/api.types';

@Component({
  selector: 'app-people',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    MatCardModule,
    MatTableModule,
    MatButtonModule,
    MatIconModule,
    MatChipsModule,
    MatFormFieldModule,
    MatInputModule,
    MatProgressSpinnerModule,
    MatSnackBarModule,
  ],
  template: `
    <h1>Pessoas e identidades</h1>

    <div class="filters">
      <mat-form-field appearance="outline">
        <mat-label>Tenant</mat-label>
        <input matInput [(ngModel)]="tenant" (change)="reload()" placeholder="acme" />
      </mat-form-field>
      <button mat-stroked-button (click)="reload()">
        <mat-icon>refresh</mat-icon> Atualizar
      </button>
    </div>

    @if (loading()) {
      <mat-progress-spinner mode="indeterminate" diameter="40" />
    } @else {
      <!-- Sugestões de automatch -->
      @if (suggestions().length > 0) {
        <mat-card appearance="outlined" class="block">
          <mat-card-header>
            <mat-card-title>
              Sugestões de merge ({{ suggestions().length }})
            </mat-card-title>
            <mat-card-subtitle>
              Identidades não-linkadas que provavelmente são a mesma pessoa.
              Aplique para criar uma nova pessoa vinculando as duas.
            </mat-card-subtitle>
          </mat-card-header>
          <mat-card-content>
            <table mat-table [dataSource]="suggestions()">
              <ng-container matColumnDef="gitlab">
                <th mat-header-cell *matHeaderCellDef>GitLab</th>
                <td mat-cell *matCellDef="let s">
                  <mat-chip class="kind-gitlab">{{ pickGitLab(s).externalUsername }}</mat-chip>
                  <span class="muted">{{ pickGitLab(s).externalEmail ?? '' }}</span>
                </td>
              </ng-container>
              <ng-container matColumnDef="jira">
                <th mat-header-cell *matHeaderCellDef>Jira</th>
                <td mat-cell *matCellDef="let s">
                  <mat-chip class="kind-jira">{{ pickJira(s).externalUsername }}</mat-chip>
                  <span class="muted">{{ pickJira(s).externalEmail ?? '' }}</span>
                </td>
              </ng-container>
              <ng-container matColumnDef="reason">
                <th mat-header-cell *matHeaderCellDef>Razão</th>
                <td mat-cell *matCellDef="let s">
                  {{ s.reason }} ({{ (s.score * 100).toFixed(0) }}%)
                </td>
              </ng-container>
              <ng-container matColumnDef="action">
                <th mat-header-cell *matHeaderCellDef></th>
                <td mat-cell *matCellDef="let s">
                  <button mat-stroked-button color="primary" (click)="applySuggestion(s)">
                    Criar pessoa + vincular
                  </button>
                </td>
              </ng-container>
              <tr mat-header-row *matHeaderRowDef="suggestionCols"></tr>
              <tr mat-row *matRowDef="let row; columns: suggestionCols"></tr>
            </table>
          </mat-card-content>
        </mat-card>
      }

      <!-- Pessoas + identidades vinculadas -->
      <mat-card appearance="outlined" class="block">
        <mat-card-header>
          <mat-card-title>
            Pessoas ({{ people().length }})
          </mat-card-title>
        </mat-card-header>
        <mat-card-content>
          @if (people().length === 0) {
            <p class="muted">
              Nenhuma pessoa cadastrada. Aplique sugestões acima ou crie via CLI.
            </p>
          } @else {
            @for (p of people(); track p.id) {
              <div class="person-row">
                <div>
                  <strong>{{ p.displayName }}</strong>
                  @if (p.primaryEmail) {
                    <span class="muted">· {{ p.primaryEmail }}</span>
                  }
                </div>
                <div class="identities">
                  @for (id of p.identities; track id.id) {
                    <mat-chip [class]="'kind-' + id.kind">
                      {{ id.kind }}: {{ id.externalUsername }}
                    </mat-chip>
                  }
                  @if (p.identities.length === 0) {
                    <span class="muted">(nenhuma identidade vinculada)</span>
                  }
                </div>
              </div>
            }
          }
        </mat-card-content>
      </mat-card>

      <!-- Identidades unlinked -->
      <mat-card appearance="outlined" class="block">
        <mat-card-header>
          <mat-card-title>
            Identidades não-vinculadas ({{ unlinked().length }})
          </mat-card-title>
          <mat-card-subtitle>
            Vistas em commits/deploys/incidents mas ainda sem pessoa canônica.
          </mat-card-subtitle>
        </mat-card-header>
        <mat-card-content>
          @if (unlinked().length === 0) {
            <p class="muted">Todas as identidades estão vinculadas.</p>
          } @else {
            <table mat-table [dataSource]="unlinked()">
              <ng-container matColumnDef="kind">
                <th mat-header-cell *matHeaderCellDef>Origem</th>
                <td mat-cell *matCellDef="let i">
                  <mat-chip [class]="'kind-' + i.kind">{{ i.kind }}</mat-chip>
                </td>
              </ng-container>
              <ng-container matColumnDef="username">
                <th mat-header-cell *matHeaderCellDef>Usuário</th>
                <td mat-cell *matCellDef="let i">{{ i.externalUsername }}</td>
              </ng-container>
              <ng-container matColumnDef="email">
                <th mat-header-cell *matHeaderCellDef>Email</th>
                <td mat-cell *matCellDef="let i">{{ i.externalEmail ?? '—' }}</td>
              </ng-container>
              <ng-container matColumnDef="action">
                <th mat-header-cell *matHeaderCellDef></th>
                <td mat-cell *matCellDef="let i">
                  <button mat-button (click)="createFromIdentity(i)">
                    Criar nova pessoa
                  </button>
                </td>
              </ng-container>
              <tr mat-header-row *matHeaderRowDef="unlinkedCols"></tr>
              <tr mat-row *matRowDef="let row; columns: unlinkedCols"></tr>
            </table>
          }
        </mat-card-content>
      </mat-card>
    }
  `,
  styles: [
    `
      .filters {
        display: flex;
        align-items: center;
        gap: 16px;
        margin: 16px 0;
      }
      .block {
        margin-top: 24px;
      }
      table {
        width: 100%;
      }
      .person-row {
        display: flex;
        justify-content: space-between;
        gap: 12px;
        padding: 8px 0;
        border-bottom: 1px solid #eee;
        align-items: center;
      }
      .person-row:last-child {
        border-bottom: none;
      }
      .identities {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
      }
      .muted {
        color: #888;
        font-size: 0.875rem;
        margin-left: 8px;
      }
      mat-chip.kind-gitlab {
        background: #fc6d26;
        color: white;
      }
      mat-chip.kind-jira {
        background: #0052cc;
        color: white;
      }
    `,
  ],
})
export class PeopleComponent {
  private api = inject(ApiClient);
  private snack = inject(MatSnackBar);

  tenant = 'acme';
  loading = signal(false);
  people = signal<PersonWithIdentities[]>([]);
  unlinked = signal<Identity[]>([]);
  suggestions = signal<MergeSuggestion[]>([]);

  suggestionCols = ['gitlab', 'jira', 'reason', 'action'];
  unlinkedCols = ['kind', 'username', 'email', 'action'];

  constructor() {
    this.reload();
  }

  reload(): void {
    if (!this.tenant) return;
    this.loading.set(true);
    forkJoin({
      people: this.api.listPeople(this.tenant).pipe(catchError(() => of([]))),
      unlinked: this.api
        .listUnlinkedIdentities(this.tenant)
        .pipe(catchError(() => of([]))),
      suggestions: this.api
        .getAutomatchSuggestions(this.tenant)
        .pipe(catchError(() => of([]))),
    })
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe(({ people, unlinked, suggestions }) => {
        this.people.set(people);
        this.unlinked.set(unlinked);
        this.suggestions.set(suggestions);
      });
  }

  pickGitLab(s: MergeSuggestion): Identity {
    return s.a.kind === 'gitlab' ? s.a : s.b;
  }

  pickJira(s: MergeSuggestion): Identity {
    return s.a.kind === 'jira' ? s.a : s.b;
  }

  applySuggestion(s: MergeSuggestion): void {
    const gl = this.pickGitLab(s);
    const jr = this.pickJira(s);
    const displayName = gl.externalEmail ?? jr.externalEmail ?? gl.externalUsername;
    const email = gl.externalEmail ?? jr.externalEmail ?? '';

    this.api
      .createPerson({
        tenant: this.tenant,
        displayName,
        primaryEmail: email,
        identityIds: [gl.id, jr.id],
      })
      .subscribe({
        next: () => {
          this.snack.open(`Pessoa criada e ${gl.kind}/${jr.kind} vinculados`, 'OK', {
            duration: 3000,
          });
          this.reload();
        },
        error: (err) => {
          this.snack.open(`Erro: ${err?.error ?? err?.message ?? err}`, 'OK', {
            duration: 5000,
          });
        },
      });
  }

  createFromIdentity(i: Identity): void {
    this.api
      .createPerson({
        tenant: this.tenant,
        displayName: i.externalEmail ?? i.externalUsername,
        primaryEmail: i.externalEmail ?? '',
        identityIds: [i.id],
      })
      .subscribe({
        next: () => {
          this.snack.open('Pessoa criada e identidade vinculada', 'OK', {
            duration: 3000,
          });
          this.reload();
        },
        error: (err) => {
          this.snack.open(`Erro: ${err?.error ?? err?.message ?? err}`, 'OK', {
            duration: 5000,
          });
        },
      });
  }
}
