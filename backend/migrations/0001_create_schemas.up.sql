-- Cria schemas separados por preocupação.
-- platform: entidades de domínio (tenant, project, team, ...)
-- raw:      eventos brutos vindos de webhooks/polling
-- metrics:  agregações pré-computadas

CREATE SCHEMA IF NOT EXISTS platform;
CREATE SCHEMA IF NOT EXISTS raw;
CREATE SCHEMA IF NOT EXISTS metrics;

-- pgcrypto é nativo no Postgres 18 mas a extensão precisa ser ativada
-- caso se queira usar gen_random_uuid() / digest() no SQL.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
