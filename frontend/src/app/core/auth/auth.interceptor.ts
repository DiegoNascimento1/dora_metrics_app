import { HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { switchMap, take } from 'rxjs/operators';

import { AuthService } from './auth.service';

/**
 * Interceptor HTTP que injeta Authorization: Bearer <access_token> em
 * todas as requests para `/api/`. Se OIDC está desligado (dev mode) ou
 * o usuário não está autenticado ainda, deixa a request passar sem
 * modificar.
 */
export const authInterceptor: HttpInterceptorFn = (req, next) => {
  const auth = inject(AuthService);

  if (!auth.enabled || !req.url.startsWith('/api/')) {
    return next(req);
  }

  return auth.getAccessToken().pipe(
    take(1),
    switchMap((token) => {
      if (!token) return next(req);
      return next(
        req.clone({ setHeaders: { Authorization: `Bearer ${token}` } }),
      );
    }),
  );
};
