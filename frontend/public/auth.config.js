// Runtime config para OIDC. Carregado ANTES do bundle Angular via
// <script src="assets/auth.config.js"></script> em index.html.
//
// Para habilitar autenticação em produção, sobrescrever este arquivo
// (mount Docker, ConfigMap Kubernetes, ou rebuild com sed) com:
//
// window.__doraAuthConfig = {
//   enabled: true,
//   authority: "https://auth.acme.com/realms/dora",
//   clientId: "dora-frontend",
//   scope: "openid profile email"
// };
//
// Default ("enabled: false") = roda sem login (modo dev).

window.__doraAuthConfig = {
  enabled: false,
  authority: "",
  clientId: ""
};
