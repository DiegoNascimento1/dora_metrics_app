-- Permite que o token de auth seja gravado direto no source_instance
-- (configurado via UI/REST) em vez de obrigar uma env var carregada via
-- secret.Provider.
--
-- TRADE-OFF documentado em docs/adr/0001-stack-go-angular.md § Mitigação:
-- secret_value é plaintext no banco no MVP. Em produção, prever:
--   1. Cifrar com chave do KMS/Vault antes do INSERT
--   2. Mover para um secret_store dedicado (HashiCorp Vault, AWS SM)
-- O collector verifica secret_value primeiro; se NULL, cai pra
-- secret.Provider.Get(auth_ref) — backward compatible com env vars.

ALTER TABLE platform.source_instance
  ADD COLUMN secret_value TEXT,
  ADD COLUMN auth_email   TEXT;

COMMENT ON COLUMN platform.source_instance.secret_value IS
  'Token plaintext gravado via UI. Sensível — não logar nem expor em respostas REST.';
COMMENT ON COLUMN platform.source_instance.auth_email IS
  'Email associado ao token (necessário para Jira Basic auth). NULL para GitLab.';
