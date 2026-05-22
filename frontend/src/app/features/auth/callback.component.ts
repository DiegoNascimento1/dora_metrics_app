import {
  ChangeDetectionStrategy,
  Component,
  OnInit,
  inject,
} from '@angular/core';
import { Router } from '@angular/router';

import { AuthService } from '../../core/auth/auth.service';

/**
 * Página de callback OIDC. A lib angular-auth-oidc-client processa o
 * `?code=...&state=...` automaticamente quando `checkAuth()` é chamado.
 * Aqui só esperamos a inicialização terminar e redirecionamos para o
 * dashboard.
 */
@Component({
  selector: 'app-auth-callback',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="callback">
      <p>Validando sessão...</p>
    </div>
  `,
  styles: [
    `
      .callback {
        text-align: center;
        padding: var(--space-6);
        color: var(--color-text-secondary);
      }
    `,
  ],
})
export class AuthCallbackComponent implements OnInit {
  private auth = inject(AuthService);
  private router = inject(Router);

  ngOnInit(): void {
    this.auth.initialize().subscribe(() => {
      void this.router.navigateByUrl('/dashboard');
    });
  }
}
