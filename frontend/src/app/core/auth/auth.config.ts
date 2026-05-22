/**
 * Configuração OIDC para autenticação via IdP externo (Keycloak, Auth0,
 * Okta, Azure AD).
 *
 * Os valores vêm do `window.__doraAuthConfig` que é injetado em runtime
 * via `public/auth.config.js` — pode ser sobrescrito por Docker mount
 * ou ConfigMap em produção, sem rebuild.
 *
 * Quando `enabled=false` (default em dev sem IdP), o frontend pula o
 * fluxo de login completamente e roda no modo "open" — o backend ainda
 * pode estar atrás de OIDC via gateway.
 */

import {
  PassedInitialConfig,
  LogLevel,
  OpenIdConfiguration,
} from 'angular-auth-oidc-client';

export interface RuntimeAuthConfig {
  /** Liga ou desliga toda a camada OIDC. False em dev. */
  enabled: boolean;
  /** URL do IdP (ex: "https://auth.acme.com/realms/dora"). */
  authority: string;
  /** Client ID registrado no IdP. */
  clientId: string;
  /** Escopos OIDC (default: "openid profile email"). */
  scope?: string;
}

declare global {
  interface Window {
    __doraAuthConfig?: RuntimeAuthConfig;
  }
}

/** Default seguro: OIDC desligado se a configuração não vier do runtime. */
const DEFAULT_CONFIG: RuntimeAuthConfig = {
  enabled: false,
  authority: '',
  clientId: '',
};

export function getRuntimeAuthConfig(): RuntimeAuthConfig {
  if (typeof window === 'undefined') return DEFAULT_CONFIG;
  return { ...DEFAULT_CONFIG, ...(window.__doraAuthConfig ?? {}) };
}

/**
 * Adapta `RuntimeAuthConfig` para `OpenIdConfiguration` da lib
 * angular-auth-oidc-client. Usa Authorization Code Flow com PKCE
 * (recomendação OAuth 2.1 — sem implicit, sem hybrid).
 */
export function buildOidcConfig(rt: RuntimeAuthConfig): OpenIdConfiguration {
  return {
    authority: rt.authority,
    redirectUrl: `${window.location.origin}/auth/callback`,
    postLogoutRedirectUri: window.location.origin,
    clientId: rt.clientId,
    scope: rt.scope ?? 'openid profile email',
    responseType: 'code',
    silentRenew: true,
    useRefreshToken: true,
    renewTimeBeforeTokenExpiresInSeconds: 60,
    logLevel: LogLevel.Warn,
    // PKCE é mandatório no Auth Code Flow; a lib usa automaticamente
    // quando responseType=code + useRefreshToken=true.
  };
}

/** Provider que a lib espera no bootstrap. Devolve config vazia se desligado. */
export const oidcProvider: PassedInitialConfig = {
  config: buildOidcConfig(getRuntimeAuthConfig()),
};
