-- Marca quando cada projeto foi sincronizado pela última vez pelo coletor.
-- Permite janelas incrementais (?updated_after=last_synced_at) em vez de varrer 30d a cada ciclo.

ALTER TABLE platform.project
  ADD COLUMN last_synced_at TIMESTAMPTZ;

CREATE INDEX project_active_last_synced_idx
  ON platform.project (last_synced_at NULLS FIRST)
  WHERE active;
