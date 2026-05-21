import {
  ChangeDetectionStrategy,
  Component,
  inject,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import {
  MAT_DIALOG_DATA,
  MatDialogModule,
  MatDialogRef,
} from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';

import { Team } from '../../core/api/api.types';

export interface TeamDialogData {
  tenant: string;
  team?: Team; // se presente: editando; senão: criando
}

export interface TeamDialogResult {
  mode: 'create' | 'update';
  slug: string;
  name: string;
  color: string;
  emoji: string;
}

// Paleta sugerida — Tailwind 500s, atendem WCAG AA contra branco.
const SUGGESTED_COLORS = [
  '#2563eb', // blue
  '#16a34a', // green
  '#d97706', // amber
  '#dc2626', // red
  '#7c3aed', // violet
  '#0891b2', // cyan
  '#db2777', // pink
  '#475569', // slate
];

const SUGGESTED_EMOJIS = ['🚀', '🛡️', '⚡', '🔥', '🎯', '🏗️', '📦', '💎', '🌊', '🦊'];

@Component({
  selector: 'app-team-dialog',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    MatDialogModule,
    MatFormFieldModule,
    MatInputModule,
    MatButtonModule,
    MatIconModule,
  ],
  template: `
    <h2 mat-dialog-title>
      {{ data.team ? 'Editar time' : 'Novo time' }}
    </h2>

    <mat-dialog-content class="content">
      <div class="preview" [style.background]="form.color || '#475569'">
        <span class="preview-emoji">{{ form.emoji || '👥' }}</span>
        <span class="preview-name">{{ form.name || 'Nome do time' }}</span>
      </div>

      <mat-form-field appearance="outline">
        <mat-label>Nome</mat-label>
        <input matInput [(ngModel)]="form.name" placeholder="Payments" />
      </mat-form-field>

      <mat-form-field appearance="outline">
        <mat-label>Slug (URL-friendly)</mat-label>
        <input
          matInput
          [(ngModel)]="form.slug"
          [disabled]="!!data.team"
          placeholder="payments"
        />
        @if (!data.team) {
          <mat-hint>Imutável após criação</mat-hint>
        }
      </mat-form-field>

      <div class="palette-block">
        <label class="palette-label">Cor</label>
        <div class="palette">
          @for (c of suggestedColors; track c) {
            <button
              type="button"
              class="swatch"
              [style.background]="c"
              [class.selected]="form.color === c"
              (click)="form.color = c"
              [attr.aria-label]="'Selecionar cor ' + c"
            ></button>
          }
        </div>
      </div>

      <div class="palette-block">
        <label class="palette-label">Emoji</label>
        <div class="palette">
          @for (e of suggestedEmojis; track e) {
            <button
              type="button"
              class="emoji"
              [class.selected]="form.emoji === e"
              (click)="form.emoji = e"
              [attr.aria-label]="'Selecionar emoji ' + e"
            >
              {{ e }}
            </button>
          }
        </div>
      </div>
    </mat-dialog-content>

    <mat-dialog-actions align="end" class="actions">
      <button mat-button mat-dialog-close type="button">Cancelar</button>
      <button
        mat-flat-button
        color="primary"
        type="button"
        (click)="save()"
        [disabled]="!canSave()"
      >
        {{ data.team ? 'Salvar alterações' : 'Criar time' }}
      </button>
    </mat-dialog-actions>
  `,
  styles: [
    `
      :host {
        display: block;
        min-width: 460px;
      }
      h2[mat-dialog-title] {
        font-weight: 700;
        letter-spacing: -0.01em;
      }
      .content {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      mat-form-field {
        width: 100%;
      }
      .preview {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        padding: var(--space-4);
        border-radius: var(--radius-lg);
        color: white;
        font-weight: 600;
        transition: background var(--transition-base);
      }
      .preview-emoji {
        font-size: 28px;
        line-height: 1;
      }
      .preview-name {
        font-size: var(--font-size-lg);
        letter-spacing: -0.01em;
      }
      .palette-block {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
      }
      .palette-label {
        font-size: var(--font-size-sm);
        font-weight: 500;
        color: var(--color-text-secondary);
      }
      .palette {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
      }
      .swatch {
        width: 36px;
        height: 36px;
        border-radius: var(--radius-md);
        border: 2px solid transparent;
        cursor: pointer;
        padding: 0;
        transition: transform var(--transition-fast), border-color var(--transition-fast);
      }
      .swatch:hover {
        transform: scale(1.08);
      }
      .swatch.selected {
        border-color: var(--color-text-primary);
        transform: scale(1.08);
      }
      .emoji {
        width: 36px;
        height: 36px;
        border-radius: var(--radius-md);
        border: 1px solid var(--color-border);
        background: var(--color-bg-elevated);
        cursor: pointer;
        font-size: 20px;
        line-height: 1;
        padding: 0;
        transition: border-color var(--transition-fast), background var(--transition-fast);
      }
      .emoji:hover {
        border-color: var(--color-brand);
      }
      .emoji.selected {
        background: var(--color-brand-subtle);
        border-color: var(--color-brand);
      }
      .actions {
        gap: var(--space-2);
        padding: var(--space-3) var(--space-5);
      }
    `,
  ],
})
export class TeamDialogComponent {
  private dialogRef = inject(MatDialogRef<TeamDialogComponent, TeamDialogResult>);
  protected data = inject<TeamDialogData>(MAT_DIALOG_DATA);

  protected suggestedColors = SUGGESTED_COLORS;
  protected suggestedEmojis = SUGGESTED_EMOJIS;

  form = {
    slug: this.data.team?.slug ?? '',
    name: this.data.team?.name ?? '',
    color: this.data.team?.color ?? SUGGESTED_COLORS[0],
    emoji: this.data.team?.emoji ?? SUGGESTED_EMOJIS[0],
  };

  canSave(): boolean {
    return this.form.name.trim().length > 0 && this.form.slug.trim().length > 0;
  }

  save(): void {
    this.dialogRef.close({
      mode: this.data.team ? 'update' : 'create',
      slug: this.form.slug.trim(),
      name: this.form.name.trim(),
      color: this.form.color,
      emoji: this.form.emoji,
    });
  }
}
