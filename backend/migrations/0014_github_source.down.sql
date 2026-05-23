-- Reverte a expansão do CHECK constraint para excluir 'github'.
--
-- ATENÇÃO: esta migration remove a capacidade de cadastrar source_instances
-- do tipo 'github'. Linhas existentes com kind='github' causarão erro no
-- próximo upsert — remova-as manualmente antes de rodar o down.

ALTER TABLE platform.source_instance
  DROP CONSTRAINT IF EXISTS source_instance_kind_check;

ALTER TABLE platform.source_instance
  ADD CONSTRAINT source_instance_kind_check
    CHECK (kind IN ('gitlab', 'jira'));
