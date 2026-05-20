-- Identidades unificadas (Fase 3.5): uma pessoa real pode ter múltiplas
-- identidades nos sistemas externos (GitLab + Jira). person é a identidade
-- canônica; person_identity é o vínculo N:1 com sistemas externos.
--
-- Documentação: ../../docs/07-roadmap.md § Fase 3.5

CREATE TABLE platform.person (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     UUID NOT NULL REFERENCES platform.tenant(id) ON DELETE CASCADE,
  display_name  TEXT NOT NULL,
  primary_email TEXT,
  avatar_url    TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX person_tenant_idx ON platform.person (tenant_id);
CREATE INDEX person_email_idx  ON platform.person (tenant_id, lower(primary_email));

CREATE TABLE platform.person_identity (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id          UUID NOT NULL REFERENCES platform.tenant(id) ON DELETE CASCADE,
  person_id          UUID REFERENCES platform.person(id) ON DELETE SET NULL,
  source_instance_id UUID REFERENCES platform.source_instance(id) ON DELETE CASCADE,
  kind               TEXT NOT NULL CHECK (kind IN ('gitlab', 'jira')),
  -- external_id é a chave estável no sistema externo (GitLab user ID, Jira accountId).
  -- Quando vier de backfill (sem chamar API), pode ser NULL e usamos external_username.
  external_id        TEXT,
  external_username  TEXT NOT NULL,
  external_email     TEXT,
  linked_at          TIMESTAMPTZ,
  linked_by          TEXT,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, kind, external_username)
);

CREATE INDEX person_identity_unlinked_idx
  ON platform.person_identity (tenant_id, kind)
  WHERE person_id IS NULL;

CREATE INDEX person_identity_person_idx
  ON platform.person_identity (person_id);

CREATE INDEX person_identity_email_idx
  ON platform.person_identity (tenant_id, lower(external_email))
  WHERE external_email IS NOT NULL;
