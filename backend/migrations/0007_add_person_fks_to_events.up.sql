-- Adiciona FKs nullable das pessoas canônicas (Fase 3.5) nos eventos. Permite
-- métricas por pessoa sem JOIN em person_identity a cada query.
--
-- Populado por:
--   * cli people link --identity X --person Y  (propaga imediatamente)
--   * cli people propagate --tenant X          (backfill em lote)
--
-- Ficam NULL enquanto a identidade correspondente não tiver sido linkada.

ALTER TABLE platform.merge_request
  ADD COLUMN author_person_id UUID REFERENCES platform.person(id) ON DELETE SET NULL;

ALTER TABLE platform.deployment
  ADD COLUMN triggerer_person_id UUID REFERENCES platform.person(id) ON DELETE SET NULL;

CREATE INDEX merge_request_author_person_idx
  ON platform.merge_request (author_person_id)
  WHERE author_person_id IS NOT NULL;

CREATE INDEX deployment_triggerer_person_idx
  ON platform.deployment (triggerer_person_id)
  WHERE triggerer_person_id IS NOT NULL;
