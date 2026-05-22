import {
  ApplicationConfig,
  provideZonelessChangeDetection,
} from '@angular/core';
import { provideRouter, withComponentInputBinding } from '@angular/router';
import { provideHttpClient, withFetch, withInterceptors } from '@angular/common/http';
import { provideAnimationsAsync } from '@angular/platform-browser/animations/async';
import { MAT_ICON_DEFAULT_OPTIONS } from '@angular/material/icon';
import { provideAuth } from 'angular-auth-oidc-client';

import { routes } from './app.routes';
import { oidcProvider } from './core/auth/auth.config';
import { authInterceptor } from './core/auth/auth.interceptor';

export const appConfig: ApplicationConfig = {
  providers: [
    provideZonelessChangeDetection(),
    provideRouter(routes, withComponentInputBinding()),
    provideHttpClient(withFetch(), withInterceptors([authInterceptor])),
    provideAnimationsAsync(),
    provideAuth(oidcProvider),
    // Material 21 default fontSet é "material-icons" (clássico, não
    // carregamos). Trocamos pra "material-symbols-outlined" + adicionamos
    // "mat-ligature-font" pra Material reconhecer como fonte de ligature
    // e usar o path correto (essencial pra <mat-icon fontIcon="nome"></mat-icon> ainda
    // funcionar sem precisar mudar todos os templates pra fontIcon="nome").
    {
      provide: MAT_ICON_DEFAULT_OPTIONS,
      useValue: { fontSet: 'material-symbols-outlined mat-ligature-font' },
    },
  ],
};
