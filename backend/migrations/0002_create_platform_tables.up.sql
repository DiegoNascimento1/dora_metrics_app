-- Entidades centrais da plataforma.
-- Detalhes: ../../docs/06-data-model.md

CREATE TABLE platform.tenant (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug        TEXT NOT NULL UNIQUE,
  name        TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE platform.source_instance (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     UUID NOT NULL REFERENCES platform.tenant(id) ON DELETE CASCADE,
  kind          TEXT NOT NULL CHECK (kind IN ('gitlab', 'jira')),
  base_url      TEXT NOT NULL,
  display_name  TEXT NOT NULL,
  auth_ref      TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, kind, base_url)
);

CREATE TABLE platform.team (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  UUID NOT NULL REFERENCES platform.tenant(id) ON DELETE CASCADE,
  slug       TEXT NOT NULL,
  name       TEXT NOT NULL,
  UNIQUE (tenant_id, slug)
);

CREATE TABLE platform.project (
  id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id               UUID NOT NULL REFERENCES platform.tenant(id) ON DELETE CASCADE,
  team_id                 UUID REFERENCES platform.team(id) ON DELETE SET NULL,
  source_instance_id      UUID NOT NULL REFERENCES platform.source_instance(id) ON DELETE CASCADE,
  external_id             TEXT NOT NULL,
  path_with_namespace     TEXT NOT NULL,
  default_branch          TEXT NOT NULL DEFAULT 'main',
  -- D3 default: regex configurável de ambiente "produção"
  production_env_pattern  TEXT NOT NULL DEFAULT '^prod(uction)?(-[a-z0-9-]+)?$',
  -- D4 default: JQL para identificar incidente
  incident_jql            TEXT NOT NULL DEFAULT 'issuetype = "Incident"',
  jira_project_keys       TEXT[] NOT NULL DEFAULT '{}',
  active                  BOOLEAN NOT NULL DEFAULT true,
  created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (source_instance_id, external_id)
);

CREATE TABLE platform.environment (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id      UUID NOT NULL REFERENCES platform.project(id) ON DELETE CASCADE,
  name            TEXT NOT NULL,
  is_production   BOOLEAN NOT NULL,
  external_id     TEXT NOT NULL,
  first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (project_id, external_id)
);

CREATE TABLE platform.merge_request (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id          UUID NOT NULL REFERENCES platform.project(id) ON DELETE CASCADE,
  external_id         TEXT NOT NULL,
  iid                 INT NOT NULL,
  title               TEXT NOT NULL,
  author_username     TEXT,
  author_is_bot       BOOLEAN NOT NULL DEFAULT false,
  target_branch       TEXT NOT NULL,
  source_branch       TEXT,
  merged_at           TIMESTAMPTZ,
  merge_commit_sha    TEXT,
  squash_commit_sha   TEXT,
  first_commit_at     TIMESTAMPTZ,
  first_commit_sha    TEXT,
  additions           INT,
  deletions           INT,
  labels              TEXT[] NOT NULL DEFAULT '{}',
  web_url             TEXT,
  raw_payload         JSONB,
  UNIQUE (project_id, external_id)
);

CREATE INDEX merge_request_project_merged_at_idx
  ON platform.merge_request (project_id, merged_at);

CREATE INDEX merge_request_target_merged_idx
  ON platform.merge_request (project_id, target_branch, merged_at);

CREATE TABLE platform.deployment (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id        UUID NOT NULL REFERENCES platform.project(id) ON DELETE CASCADE,
  environment_id    UUID NOT NULL REFERENCES platform.environment(id) ON DELETE CASCADE,
  external_id       TEXT NOT NULL,
  sha               TEXT NOT NULL,
  ref               TEXT,
  status            TEXT NOT NULL,
  triggered_by      TEXT,
  started_at        TIMESTAMPTZ,
  finished_at       TIMESTAMPTZ,
  is_rollback       BOOLEAN NOT NULL DEFAULT false,
  raw_payload       JSONB,
  UNIQUE (project_id, external_id)
);

CREATE INDEX deployment_env_status_finished_idx
  ON platform.deployment (environment_id, status, finished_at);

CREATE INDEX deployment_project_finished_idx
  ON platform.deployment (project_id, finished_at DESC);

CREATE TABLE platform.deployment_mr_link (
  deployment_id     UUID NOT NULL REFERENCES platform.deployment(id) ON DELETE CASCADE,
  merge_request_id  UUID NOT NULL REFERENCES platform.merge_request(id) ON DELETE CASCADE,
  PRIMARY KEY (deployment_id, merge_request_id)
);

CREATE TABLE platform.incident (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id             UUID NOT NULL REFERENCES platform.tenant(id) ON DELETE CASCADE,
  source_instance_id    UUID NOT NULL REFERENCES platform.source_instance(id) ON DELETE CASCADE,
  external_id           TEXT NOT NULL,
  jira_project_key      TEXT NOT NULL,
  summary               TEXT NOT NULL,
  status                TEXT NOT NULL,
  status_category       TEXT NOT NULL,
  priority              TEXT,
  issuetype             TEXT,
  labels                TEXT[] NOT NULL DEFAULT '{}',
  created_at            TIMESTAMPTZ NOT NULL,
  resolved_at           TIMESTAMPTZ,
  raw_payload           JSONB,
  UNIQUE (source_instance_id, external_id)
);

CREATE INDEX incident_tenant_created_idx
  ON platform.incident (tenant_id, created_at);

CREATE INDEX incident_tenant_resolved_idx
  ON platform.incident (tenant_id, resolved_at);

CREATE TABLE platform.deployment_incident_link (
  deployment_id  UUID NOT NULL REFERENCES platform.deployment(id) ON DELETE CASCADE,
  incident_id    UUID NOT NULL REFERENCES platform.incident(id) ON DELETE CASCADE,
  link_reason    TEXT NOT NULL,
  linked_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (deployment_id, incident_id)
);

CREATE TABLE platform.classification_threshold (
  tenant_id  UUID PRIMARY KEY REFERENCES platform.tenant(id) ON DELETE CASCADE,
  config     JSONB NOT NULL
);
