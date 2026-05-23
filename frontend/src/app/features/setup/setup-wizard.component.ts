import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  signal,
  OnDestroy,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { MatStepperModule } from '@angular/material/stepper';
import { MatCardModule } from '@angular/material/card';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatChipsModule } from '@angular/material/chips';
import { MatCheckboxModule } from '@angular/material/checkbox';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { catchError, finalize, of } from 'rxjs';

import { ApiClient } from '../../core/api/api.client';
import {
  CreateSourceInstanceRequest,
  Project,
  SourceInstance,
  TestConnectionResponse,
} from '../../core/api/api.types';

type ProviderKind = 'gitlab' | 'github' | 'bitbucket' | 'azuredevops';

interface ProviderCard {
  id: ProviderKind;
  label: string;
  icon: string;
  description: string;
  disabled: boolean;
  comingSoon: boolean;
}

@Component({
  selector: 'app-setup-wizard',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    FormsModule,
    RouterLink,
    MatStepperModule,
    MatCardModule,
    MatButtonModule,
    MatIconModule,
    MatFormFieldModule,
    MatInputModule,
    MatChipsModule,
    MatCheckboxModule,
    MatProgressBarModule,
    MatProgressSpinnerModule,
    MatSnackBarModule,
  ],
  template: `
    <div class="wizard-host">
      <div class="wizard-header">
        <mat-icon class="wizard-logo" fontIcon="speed"></mat-icon>
        <h1 class="wizard-title">Configure seu workspace</h1>
        <p class="wizard-subtitle">
          Siga os 4 passos para começar a visualizar suas métricas DORA
        </p>
      </div>

      <mat-stepper
        class="wizard-stepper"
        [orientation]="stepperOrientation()"
        linear
        #stepper
      >
        <!-- ===== STEP 1: Workspace ===== -->
        <mat-step label="Workspace" [completed]="step1Done()">
          <div class="step-content">
            <h2 class="step-title">Crie seu workspace</h2>
            <p class="step-desc">
              Um workspace (tenant) agrupa seus projetos, times e métricas.
              Escolha um nome claro — pode ser o nome da sua empresa ou squad.
            </p>

            <div class="form-group">
              <mat-form-field appearance="outline" class="full-width">
                <mat-label>Nome do workspace</mat-label>
                <input
                  matInput
                  [(ngModel)]="workspaceName"
                  (ngModelChange)="onWorkspaceNameChange($event)"
                  placeholder="Minha Empresa"
                  maxlength="64"
                />
                <mat-hint>Escolha um nome fácil de reconhecer</mat-hint>
              </mat-form-field>

              @if (workspaceName) {
                <div class="slug-preview">
                  <span class="slug-label">Slug:</span>
                  <code class="slug-value">{{ workspaceSlug() }}</code>
                  <span class="slug-hint">Usado na URL e na API</span>
                </div>
              }
            </div>

            @if (step1Error()) {
              <div class="step-error">
                <mat-icon fontIcon="error_outline"></mat-icon>
                {{ step1Error() }}
              </div>
            }

            <div class="step-actions">
              <button
                mat-flat-button
                color="primary"
                [disabled]="!workspaceName || savingWorkspace()"
                (click)="createWorkspace(stepper)"
              >
                @if (savingWorkspace()) {
                  <mat-progress-spinner mode="indeterminate" diameter="18" />
                } @else {
                  <mat-icon fontIcon="arrow_forward"></mat-icon>
                }
                Próximo
              </button>
            </div>
          </div>
        </mat-step>

        <!-- ===== STEP 2: Conectar fonte ===== -->
        <mat-step label="Fonte de dados" [completed]="step2Done()">
          <div class="step-content">
            <h2 class="step-title">Conecte sua fonte de código</h2>
            <p class="step-desc">
              Selecione o provedor onde seus repositórios e deployments vivem.
            </p>

            <div class="provider-grid">
              @for (p of providers; track p.id) {
                <div
                  class="provider-card"
                  [class.selected]="selectedProvider() === p.id"
                  [class.disabled]="p.disabled"
                  (click)="!p.disabled && selectProvider(p.id)"
                  [attr.tabindex]="p.disabled ? -1 : 0"
                  (keydown.enter)="!p.disabled && selectProvider(p.id)"
                  role="button"
                  [attr.aria-pressed]="selectedProvider() === p.id"
                  [attr.aria-label]="p.label + (p.comingSoon ? ' — em breve' : '')"
                >
                  <div class="provider-icon-wrap">
                    <mat-icon [fontIcon]="p.icon" class="provider-icon"></mat-icon>
                  </div>
                  <div class="provider-body">
                    <div class="provider-name">
                      {{ p.label }}
                      @if (p.comingSoon) {
                        <mat-chip class="chip-soon">Em breve</mat-chip>
                      }
                    </div>
                    <div class="provider-desc">{{ p.description }}</div>
                  </div>
                  @if (selectedProvider() === p.id) {
                    <mat-icon class="check-mark" fontIcon="check_circle"></mat-icon>
                  }
                </div>
              }
            </div>

            @if (selectedProvider() === 'gitlab' || selectedProvider() === 'github') {
              <div class="credentials-panel">
                <h3 class="cred-title">
                  <mat-icon [fontIcon]="selectedProvider() === 'gitlab' ? 'terminal' : 'key'" class="cred-icon"></mat-icon>
                  Credenciais de acesso
                </h3>

                @if (selectedProvider() === 'gitlab') {
                  <mat-form-field appearance="outline" class="full-width">
                    <mat-label>URL da instância GitLab</mat-label>
                    <input
                      matInput
                      type="url"
                      [(ngModel)]="gitlabUrl"
                      placeholder="https://gitlab.com"
                    />
                    <mat-hint>URL base da sua instância GitLab (cloud ou self-managed)</mat-hint>
                  </mat-form-field>
                } @else {
                  <div class="fixed-url-row">
                    <mat-icon fontIcon="link" class="link-icon"></mat-icon>
                    <span>Instância: <code>https://api.github.com</code></span>
                  </div>
                }

                <mat-form-field appearance="outline" class="full-width">
                  <mat-label>Personal Access Token</mat-label>
                  <input
                    matInput
                    [type]="showToken() ? 'text' : 'password'"
                    [(ngModel)]="accessToken"
                    [placeholder]="selectedProvider() === 'gitlab' ? 'glpat-xxxx' : 'ghp_xxxx'"
                    autocomplete="off"
                  />
                  <button
                    mat-icon-button
                    matSuffix
                    type="button"
                    (click)="showToken.set(!showToken())"
                  >
                    <mat-icon [fontIcon]="showToken() ? 'visibility_off' : 'visibility'"></mat-icon>
                  </button>
                  <mat-hint>
                    @if (selectedProvider() === 'gitlab') {
                      Precisa dos escopos: read_api, read_repository
                    } @else {
                      Precisa dos escopos: repo, read:user, read:org
                    }
                  </mat-hint>
                </mat-form-field>

                @if (testResult(); as r) {
                  <div class="test-result" [class.ok]="r.ok" [class.bad]="!r.ok">
                    <mat-icon [fontIcon]="r.ok ? 'check_circle' : 'error'"></mat-icon>
                    <span>{{ r.ok ? 'Conexão bem-sucedida!' : (r.message ?? 'Falha na conexão') }}</span>
                  </div>
                }

                <div class="cred-actions">
                  <button
                    mat-stroked-button
                    [disabled]="!canTestConnection() || testing()"
                    (click)="testConnection()"
                  >
                    @if (testing()) {
                      <mat-progress-spinner mode="indeterminate" diameter="16" />
                    } @else {
                      <mat-icon fontIcon="cloud_sync"></mat-icon>
                    }
                    Testar conexão
                  </button>
                </div>
              </div>
            }

            @if (step2Error()) {
              <div class="step-error">
                <mat-icon fontIcon="error_outline"></mat-icon>
                {{ step2Error() }}
              </div>
            }

            <div class="step-actions">
              <button mat-button matStepperPrevious>Voltar</button>
              <button
                mat-flat-button
                color="primary"
                [disabled]="!canSaveSource() || savingSource()"
                (click)="saveSource(stepper)"
              >
                @if (savingSource()) {
                  <mat-progress-spinner mode="indeterminate" diameter="18" />
                }
                Próximo
              </button>
            </div>
          </div>
        </mat-step>

        <!-- ===== STEP 3: Projetos ===== -->
        <mat-step label="Projetos" [completed]="step3Done()">
          <div class="step-content">
            <h2 class="step-title">Selecione os projetos</h2>
            <p class="step-desc">
              Escolha quais repositórios deseja monitorar. Você pode adicionar
              mais depois em <strong>Projetos</strong>.
            </p>

            @if (loadingProjects()) {
              <div class="loading-projects">
                <mat-progress-spinner mode="indeterminate" diameter="32" />
                <span>Descobrindo repositórios...</span>
              </div>
            } @else if (discoveredProjects().length === 0) {
              <div class="fallback-tip">
                <div class="fallback-icon-wrap">
                  <mat-icon fontIcon="terminal" class="fallback-icon"></mat-icon>
                </div>
                <h3>Cadastre projetos via CLI</h3>
                <p>
                  Nenhum projeto foi descoberto automaticamente ainda.
                  Você pode cadastrar projetos usando o CLI:
                </p>
                <div class="code-block">
                  <code>dora project add --slug meu-projeto --gitlab-path grupo/repo --tenant {{ workspaceSlug() }}</code>
                </div>
                <p class="muted-tip">
                  Ou acesse <a routerLink="/projects">Projetos</a> após concluir
                  o wizard para cadastrar pela interface.
                </p>
              </div>
            } @else {
              <div class="project-list">
                @for (p of discoveredProjects(); track p.id) {
                  <div class="project-item">
                    <mat-checkbox
                      [checked]="isProjectSelected(p.id)"
                      (change)="toggleProject(p.id)"
                      color="primary"
                    >
                      <div class="project-item-body">
                        <span class="project-path">{{ p.pathWithNamespace }}</span>
                        <span class="project-name">{{ p.name }}</span>
                      </div>
                    </mat-checkbox>
                  </div>
                }
              </div>

              <p class="selection-hint">
                {{ selectedProjectIds().size }} de {{ discoveredProjects().length }} selecionado(s)
              </p>
            }

            <div class="step-actions">
              <button mat-button matStepperPrevious>Voltar</button>
              <button
                mat-flat-button
                color="primary"
                (click)="goToStep4(stepper)"
              >
                @if (discoveredProjects().length === 0) {
                  Pular por enquanto
                } @else {
                  Confirmar seleção
                }
              </button>
            </div>
          </div>
        </mat-step>

        <!-- ===== STEP 4: Pronto ===== -->
        <mat-step label="Pronto!">
          <div class="step-content success-content">
            <div class="success-icon-wrap">
              <mat-icon fontIcon="check_circle" class="success-icon"></mat-icon>
            </div>

            <h2 class="step-title">Tudo configurado!</h2>
            <p class="step-desc">
              Seu workspace <strong>{{ workspaceName }}</strong> está ativo.
              Estamos coletando dados históricos dos últimos 30 dias.
            </p>

            <div class="collecting-card">
              <div class="collecting-row">
                <mat-icon fontIcon="sync" class="sync-icon"></mat-icon>
                <span>Coletando dados históricos dos últimos 30 dias...</span>
              </div>
              <mat-progress-bar mode="indeterminate" class="collecting-bar" />
            </div>

            <div class="success-actions">
              <a mat-flat-button color="primary" routerLink="/dashboard">
                <mat-icon fontIcon="dashboard"></mat-icon>
                Ver dashboard
              </a>
              <a mat-stroked-button routerLink="/settings">
                <mat-icon fontIcon="settings"></mat-icon>
                Configurações avançadas
              </a>
            </div>
          </div>
        </mat-step>
      </mat-stepper>
    </div>
  `,
  styles: [
    `
      .wizard-host {
        max-width: 780px;
        margin: 0 auto;
        padding: var(--space-6) var(--space-4);
      }

      .wizard-header {
        text-align: center;
        margin-bottom: var(--space-6);
      }
      .wizard-logo {
        font-size: 48px;
        height: 48px;
        width: 48px;
        color: var(--color-brand);
        margin-bottom: var(--space-3);
      }
      .wizard-title {
        margin: 0 0 var(--space-2);
        font-size: var(--font-size-xl);
        font-weight: 700;
        letter-spacing: -0.02em;
      }
      .wizard-subtitle {
        margin: 0;
        color: var(--color-text-secondary);
      }

      .wizard-stepper {
        background: transparent;
      }

      .step-content {
        padding: var(--space-5) 0 var(--space-4);
        max-width: 620px;
      }
      .step-title {
        margin: 0 0 var(--space-2);
        font-size: var(--font-size-lg);
        font-weight: 600;
      }
      .step-desc {
        margin: 0 0 var(--space-5);
        color: var(--color-text-secondary);
        line-height: 1.6;
      }

      .form-group {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .full-width {
        width: 100%;
      }

      .slug-preview {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        padding: var(--space-2) var(--space-3);
        background: var(--color-surface-subtle);
        border-radius: var(--radius-md);
        font-size: var(--font-size-sm);
      }
      .slug-label {
        color: var(--color-text-muted);
        font-weight: 500;
      }
      .slug-value {
        background: var(--color-bg-elevated);
        padding: 2px 8px;
        border-radius: var(--radius-sm);
        border: 1px solid var(--color-border);
        font-family: var(--font-mono);
        color: var(--color-brand);
        font-weight: 600;
      }
      .slug-hint {
        color: var(--color-text-muted);
        font-size: var(--font-size-xs);
      }

      /* Provider cards */
      .provider-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
        gap: var(--space-3);
        margin-bottom: var(--space-5);
      }
      .provider-card {
        position: relative;
        display: flex;
        align-items: flex-start;
        gap: var(--space-3);
        padding: var(--space-4);
        border: 2px solid var(--color-border);
        border-radius: var(--radius-lg);
        cursor: pointer;
        transition: border-color var(--transition-base), box-shadow var(--transition-base);
        background: var(--color-bg-elevated);
        user-select: none;
      }
      .provider-card:hover:not(.disabled) {
        border-color: var(--color-brand);
        box-shadow: 0 0 0 3px var(--color-brand-subtle);
      }
      .provider-card:focus-visible {
        outline: none;
        border-color: var(--color-brand);
        box-shadow: 0 0 0 3px var(--color-brand-subtle);
      }
      .provider-card.selected {
        border-color: var(--color-brand);
        background: color-mix(in srgb, var(--color-brand) 5%, var(--color-bg-elevated));
      }
      .provider-card.disabled {
        opacity: 0.55;
        cursor: not-allowed;
        pointer-events: none;
      }
      .provider-icon-wrap {
        width: 40px;
        height: 40px;
        border-radius: var(--radius-md);
        background: var(--color-brand-subtle);
        display: flex;
        align-items: center;
        justify-content: center;
        flex-shrink: 0;
      }
      .provider-icon {
        font-size: 22px;
        height: 22px;
        width: 22px;
        color: var(--color-brand);
      }
      .provider-body {
        flex: 1;
        min-width: 0;
      }
      .provider-name {
        font-weight: 600;
        color: var(--color-text-primary);
        display: flex;
        align-items: center;
        gap: var(--space-2);
        flex-wrap: wrap;
      }
      .provider-desc {
        font-size: var(--font-size-sm);
        color: var(--color-text-muted);
        margin-top: 2px;
      }
      .chip-soon {
        font-size: 0.625rem !important;
        height: 18px !important;
        padding: 0 8px !important;
        background: var(--color-tier-medium-bg) !important;
        color: var(--color-tier-medium) !important;
        min-height: unset !important;
      }
      .check-mark {
        position: absolute;
        top: var(--space-3);
        right: var(--space-3);
        font-size: 20px;
        height: 20px;
        width: 20px;
        color: var(--color-brand);
      }

      /* Credentials panel */
      .credentials-panel {
        padding: var(--space-4);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-lg);
        background: var(--color-surface-subtle);
        margin-bottom: var(--space-4);
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .cred-title {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        margin: 0 0 var(--space-2);
        font-size: var(--font-size-base);
        font-weight: 600;
        color: var(--color-text-primary);
      }
      .cred-icon {
        font-size: 18px;
        height: 18px;
        width: 18px;
        color: var(--color-brand);
      }
      .fixed-url-row {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        padding: var(--space-2) var(--space-3);
        background: var(--color-bg-elevated);
        border-radius: var(--radius-md);
        border: 1px solid var(--color-border);
        font-size: var(--font-size-sm);
        color: var(--color-text-secondary);
      }
      .link-icon {
        font-size: 16px;
        height: 16px;
        width: 16px;
        opacity: 0.7;
      }
      .fixed-url-row code {
        font-family: var(--font-mono);
        color: var(--color-text-primary);
      }
      .test-result {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        padding: var(--space-2) var(--space-3);
        border-radius: var(--radius-md);
        font-size: var(--font-size-sm);
        font-weight: 500;
      }
      .test-result.ok {
        background: var(--color-tier-elite-bg);
        color: var(--color-tier-elite);
      }
      .test-result.bad {
        background: var(--color-tier-low-bg);
        color: var(--color-tier-low);
      }
      .cred-actions {
        display: flex;
        justify-content: flex-end;
      }

      /* Projects list */
      .loading-projects {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        padding: var(--space-5);
        color: var(--color-text-secondary);
      }
      .project-list {
        display: flex;
        flex-direction: column;
        gap: 0;
        border: 1px solid var(--color-border);
        border-radius: var(--radius-lg);
        overflow: hidden;
        margin-bottom: var(--space-3);
      }
      .project-item {
        padding: var(--space-3) var(--space-4);
        border-bottom: 1px solid var(--color-border);
        background: var(--color-bg-elevated);
        transition: background var(--transition-fast);
      }
      .project-item:last-child {
        border-bottom: none;
      }
      .project-item:hover {
        background: var(--color-surface-subtle);
      }
      .project-item-body {
        display: flex;
        flex-direction: column;
        gap: 2px;
        margin-left: var(--space-2);
      }
      .project-path {
        font-family: var(--font-mono);
        font-size: var(--font-size-sm);
        color: var(--color-text-primary);
      }
      .project-name {
        font-size: var(--font-size-xs);
        color: var(--color-text-muted);
      }
      .selection-hint {
        font-size: var(--font-size-sm);
        color: var(--color-text-secondary);
        margin: 0;
      }

      /* Fallback tip */
      .fallback-tip {
        padding: var(--space-6) var(--space-4);
        text-align: center;
        border: 2px dashed var(--color-border);
        border-radius: var(--radius-lg);
        margin-bottom: var(--space-4);
      }
      .fallback-icon-wrap {
        width: 56px;
        height: 56px;
        margin: 0 auto var(--space-3);
        border-radius: 50%;
        background: var(--color-surface-subtle);
        display: flex;
        align-items: center;
        justify-content: center;
      }
      .fallback-icon {
        font-size: 28px;
        height: 28px;
        width: 28px;
        color: var(--color-text-muted);
      }
      .fallback-tip h3 {
        margin: 0 0 var(--space-2);
        font-size: var(--font-size-base);
      }
      .fallback-tip p {
        color: var(--color-text-secondary);
        margin: 0 0 var(--space-3);
      }
      .code-block {
        background: var(--color-surface-subtle);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-md);
        padding: var(--space-3) var(--space-4);
        text-align: left;
        margin: 0 auto;
        max-width: 560px;
        overflow-x: auto;
      }
      .code-block code {
        font-family: var(--font-mono);
        font-size: var(--font-size-sm);
        color: var(--color-text-primary);
        white-space: nowrap;
      }
      .muted-tip {
        font-size: var(--font-size-sm);
        color: var(--color-text-muted);
        margin: var(--space-3) 0 0 !important;
      }

      /* Success step */
      .success-content {
        text-align: center;
        padding-top: var(--space-6);
      }
      .success-icon-wrap {
        margin-bottom: var(--space-4);
        animation: pop 0.4s cubic-bezier(0.34, 1.56, 0.64, 1) forwards;
      }
      .success-icon {
        font-size: 72px;
        height: 72px;
        width: 72px;
        color: var(--color-tier-elite);
      }
      @keyframes pop {
        from { transform: scale(0.5); opacity: 0; }
        to   { transform: scale(1);   opacity: 1; }
      }

      .collecting-card {
        margin: var(--space-5) auto;
        max-width: 480px;
        background: var(--color-surface-subtle);
        border: 1px solid var(--color-border);
        border-radius: var(--radius-lg);
        padding: var(--space-4);
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      .collecting-row {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        font-size: var(--font-size-sm);
        color: var(--color-text-secondary);
      }
      .sync-icon {
        font-size: 18px;
        height: 18px;
        width: 18px;
        color: var(--color-brand);
        animation: spin 2s linear infinite;
      }
      @keyframes spin {
        from { transform: rotate(0deg); }
        to   { transform: rotate(360deg); }
      }
      .collecting-bar {
        border-radius: 99px;
      }

      .success-actions {
        display: flex;
        justify-content: center;
        gap: var(--space-3);
        flex-wrap: wrap;
      }

      /* Step actions */
      .step-actions {
        display: flex;
        gap: var(--space-2);
        margin-top: var(--space-5);
      }
      .step-error {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        padding: var(--space-3);
        background: var(--color-tier-low-bg);
        color: var(--color-tier-low);
        border-radius: var(--radius-md);
        font-size: var(--font-size-sm);
        margin-bottom: var(--space-3);
      }

      /* Responsive — vertical on mobile */
      @media (max-width: 600px) {
        .wizard-host {
          padding: var(--space-4) var(--space-3);
        }
        .provider-grid {
          grid-template-columns: 1fr;
        }
        .success-actions {
          flex-direction: column;
          align-items: stretch;
        }
      }
    `,
  ],
})
export class SetupWizardComponent implements OnDestroy {
  private api = inject(ApiClient);
  private router = inject(Router);
  private snack = inject(MatSnackBar);

  // ---- Step 1 state ----
  workspaceName = '';
  savingWorkspace = signal(false);
  step1Done = signal(false);
  step1Error = signal<string | null>(null);

  workspaceSlug = computed(() => this.toSlug(this.workspaceName));

  // ---- Step 2 state ----
  selectedProvider = signal<ProviderKind | null>(null);
  gitlabUrl = 'https://gitlab.com';
  accessToken = '';
  showToken = signal(false);
  testing = signal(false);
  savingSource = signal(false);
  testResult = signal<TestConnectionResponse | null>(null);
  step2Done = signal(false);
  step2Error = signal<string | null>(null);
  createdSourceInstanceId = signal<string | null>(null);

  // ---- Step 3 state ----
  loadingProjects = signal(false);
  discoveredProjects = signal<Project[]>([]);
  selectedProjectIds = signal<Set<string>>(new Set());
  step3Done = signal(false);

  // ---- Screen breakpoint for stepper orientation ----
  stepperOrientation = signal<'horizontal' | 'vertical'>(
    window.innerWidth < 600 ? 'vertical' : 'horizontal',
  );

  private resizeObserver?: ResizeObserver;

  readonly providers: ProviderCard[] = [
    {
      id: 'gitlab',
      label: 'GitLab',
      icon: 'source',
      description: 'GitLab.com ou self-managed. Coleta deployments, MRs e pipelines.',
      disabled: false,
      comingSoon: false,
    },
    {
      id: 'github',
      label: 'GitHub',
      icon: 'hub',
      description: 'GitHub.com via API v3. Coleta commits, pull requests e workflows.',
      disabled: false,
      comingSoon: false,
    },
    {
      id: 'bitbucket',
      label: 'Bitbucket',
      icon: 'account_tree',
      description: 'Bitbucket Cloud ou Data Center.',
      disabled: true,
      comingSoon: true,
    },
    {
      id: 'azuredevops',
      label: 'Azure DevOps',
      icon: 'cloud_queue',
      description: 'Azure Repos + Pipelines.',
      disabled: true,
      comingSoon: true,
    },
  ];

  constructor() {
    // Observa mudança de tamanho de tela
    this.resizeObserver = new ResizeObserver(() => {
      this.stepperOrientation.set(window.innerWidth < 600 ? 'vertical' : 'horizontal');
    });
    this.resizeObserver.observe(document.body);
  }

  ngOnDestroy(): void {
    this.resizeObserver?.disconnect();
  }

  onWorkspaceNameChange(_name: string): void {
    this.step1Error.set(null);
  }

  private toSlug(name: string): string {
    return name
      .toLowerCase()
      .trim()
      .replace(/[^\w\s-]/g, '')
      .replace(/[\s_-]+/g, '-')
      .replace(/^-+|-+$/g, '');
  }

  // Step 1: create tenant (workspace)
  createWorkspace(stepper: { next: () => void }): void {
    if (!this.workspaceName) return;
    this.savingWorkspace.set(true);
    this.step1Error.set(null);

    // Tenta criar o tenant via API; ignora erro 409 (já existe)
    this.api.createTenant({ name: this.workspaceName, slug: this.workspaceSlug() })
      .pipe(
        catchError((err) => {
          // 409 = já existe: ok para continuar
          if (err?.status === 409) return of({ slug: this.workspaceSlug() });
          this.step1Error.set(err?.error?.message ?? err?.message ?? 'Erro ao criar workspace');
          return of(null);
        }),
        finalize(() => this.savingWorkspace.set(false)),
      )
      .subscribe((result) => {
        if (result) {
          this.step1Done.set(true);
          stepper.next();
        }
      });
  }

  // Step 2: provider selection
  selectProvider(id: ProviderKind): void {
    this.selectedProvider.set(id);
    this.testResult.set(null);
    this.step2Error.set(null);
  }

  canTestConnection(): boolean {
    if (!this.selectedProvider()) return false;
    if (!this.accessToken) return false;
    if (this.selectedProvider() === 'gitlab' && !this.gitlabUrl) return false;
    return true;
  }

  canSaveSource(): boolean {
    return (this.selectedProvider() !== null) && this.canTestConnection();
  }

  testConnection(): void {
    this.testing.set(true);
    this.testResult.set(null);
    const kind = this.selectedProvider() === 'github' ? 'gitlab' : 'gitlab';
    const baseUrl = this.selectedProvider() === 'github'
      ? 'https://api.github.com'
      : this.gitlabUrl;

    this.api.testConnection({
      kind: kind as 'gitlab' | 'jira',
      baseUrl,
      secret: this.accessToken,
    }).pipe(
      catchError(() => of<TestConnectionResponse>({ ok: false, message: 'Falha de rede' })),
      finalize(() => this.testing.set(false)),
    ).subscribe((r) => this.testResult.set(r));
  }

  saveSource(stepper: { next: () => void }): void {
    if (!this.canSaveSource()) return;
    this.savingSource.set(true);
    this.step2Error.set(null);

    const provider = this.selectedProvider()!;
    const baseUrl = provider === 'github'
      ? 'https://api.github.com'
      : this.gitlabUrl;
    const kind = provider === 'github' ? 'gitlab' : 'gitlab';

    const req: CreateSourceInstanceRequest = {
      tenant: this.workspaceSlug(),
      kind: kind as 'gitlab' | 'jira',
      baseUrl,
      displayName: `${provider}-${this.workspaceSlug()}`,
      secret: this.accessToken,
    };

    this.api.createSourceInstance(req).pipe(
      catchError((err) => {
        this.step2Error.set(err?.error?.message ?? 'Erro ao salvar integração');
        return of(null);
      }),
      finalize(() => this.savingSource.set(false)),
    ).subscribe((instance) => {
      if (instance) {
        this.createdSourceInstanceId.set(instance.id);
        this.step2Done.set(true);
        this.loadProjects(instance.id);
        stepper.next();
      }
    });
  }

  private loadProjects(sourceInstanceId: string): void {
    this.loadingProjects.set(true);
    this.api.listProjects().pipe(
      catchError(() => of([] as Project[])),
      finalize(() => this.loadingProjects.set(false)),
    ).subscribe((projects) => {
      this.discoveredProjects.set(projects);
      // Pré-seleciona todos
      this.selectedProjectIds.set(new Set(projects.map((p) => p.id)));
    });
  }

  isProjectSelected(id: string): boolean {
    return this.selectedProjectIds().has(id);
  }

  toggleProject(id: string): void {
    const set = new Set(this.selectedProjectIds());
    if (set.has(id)) {
      set.delete(id);
    } else {
      set.add(id);
    }
    this.selectedProjectIds.set(set);
  }

  goToStep4(stepper: { next: () => void }): void {
    this.step3Done.set(true);
    stepper.next();
  }
}
