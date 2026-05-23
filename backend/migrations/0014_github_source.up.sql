-- Fase 8: suporte a GitHub como fonte de deployments e merge requests.
--
-- platform.source_instance.kind é TEXT CHECK — basta expandir o constraint.
-- Não usamos ENUM do Postgres porque ALTER TYPE ADD VALUE não é transacional
-- (ver docs/06-data-model.md §source_instance).

ALTER TABLE platform.source_instance
  DROP CONSTRAINT IF EXISTS source_instance_kind_check;

ALTER TABLE platform.source_instance
  ADD CONSTRAINT source_instance_kind_check
    CHECK (kind IN ('gitlab', 'jira', 'github'));
