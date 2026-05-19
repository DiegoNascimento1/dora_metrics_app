# 0002 — Banco de dados: PostgreSQL puro

- **Status:** Accepted
- **Data:** 2026-05-19
- **Autores:** Diego
- **Decisores:** Diego

## Contexto

A plataforma precisa de um armazenamento que atenda três cargas distintas:

1. **Eventos brutos** (`raw_event`) — append-heavy, JSON aninhado vindo de webhooks, raramente atualizado depois.
2. **Entidades de domínio** (`project`, `deployment`, `merge_request`, `incident`) — CRUD relacional clássico com idempotência por ID externo (UPSERT).
3. **Agregações** (`metric_window`, `metric_daily`) — leitura intensiva para o dashboard, recálculo incremental ou em janelas rolantes.

Volume estimado para o MVP: até **50 projetos monitorados**, alguns milhares de eventos/dia. Em horizonte de 2 anos, idealmente até **500 projetos** e dezenas de milhares de eventos/dia.

Alternativas mais especializadas (TimescaleDB, ClickHouse) foram avaliadas em [05-architecture.md § Decisão D2](../05-architecture.md#decisão-d2--banco) e [06-data-model.md § Decisão D2](../06-data-model.md#decisão-d2--banco).

## Decisão

**PostgreSQL 18+ como único banco primário.** Sem TimescaleDB, sem ClickHouse, sem datalake no MVP.

Escopo prático:

- Uma única instância Postgres com schemas separados por preocupação (`platform`, `raw`, `metrics`).
- `JSONB` para payloads de webhook (`raw_event.payload`).
- Índices parciais (`WHERE processed_at IS NULL`) para a fila de processamento.
- `tstzrange` quando útil para janelas (não obrigatório).
- Migrations versionadas via `golang-migrate` (alinhado com [ADR 0001](0001-stack-go-angular.md)).
- Sem extensões além das nativas do Postgres no MVP (`pgcrypto` para UUID já é nativa).

## Alternativas consideradas

- **PostgreSQL + TimescaleDB** — descartado para o MVP. Particionamento automático e agregações contínuas são úteis, mas adicionam dependência operacional (extensão, versionamento próprio, backup específico) que não se justifica até termos volume real. Postgres puro consegue lidar com a escala prevista para 2 anos se modelado corretamente.

- **ClickHouse + Postgres metadata** — descartado. Complexidade significativa de modelagem (sem UPDATE/DELETE eficiente complica idempotência), dois bancos para operar, sincronização não-trivial. Faz sentido apenas em volumes acima de centenas de milhões de eventos — fora do nosso horizonte de planejamento.

- **MongoDB** — não foi considerado seriamente. Modelo relacional de DORA (deployment ↔ MR ↔ incident) se beneficia de JOINs e constraints. JSONB no Postgres dá flexibilidade suficiente para os payloads brutos.

## Consequências

### Positivas

- **Operacional simples:** uma só dependência stateful para fazer backup, monitorar e versionar.
- **Tooling maduro:** `pgx`, `sqlc`, `golang-migrate`, `pgAdmin`/`DBeaver`, `pg_dump`, replicação física e lógica — tudo testado em batalha.
- **JOINs nativos** entre entidades de domínio, sem precisar de duas fontes.
- **JSONB poderoso** — guardamos o payload original em `raw_event` e ainda conseguimos indexar campos específicos com `jsonb_path_ops` se virar gargalo.
- **Sem lock-in:** Postgres é Postgres em qualquer cloud (RDS, Cloud SQL, Azure Database) ou self-hosted.
- **Caminho de evolução claro:** se um dia precisarmos, dá pra adicionar TimescaleDB como extensão **na mesma instância** sem migrar dados.

### Negativas

- **Agregações em janelas grandes** podem ficar lentas quando `raw_event` passar de 10M linhas. Sem particionamento nativo automático, vamos precisar manter a tabela com retenção (90d online) e arquivar o resto.
- **Sem agregação contínua nativa** — precisamos implementar o recálculo de `metric_window` via job agendado, em vez de declarar uma `continuous_aggregate` como no Timescale.
- **Single point of failure:** se a instância Postgres cai, toda a plataforma cai. Mitigar com réplica de leitura/standby a partir da Fase 4 (multi-tenant).
- **Escala vertical antes de horizontal:** subir tamanho de instância é o caminho natural; particionar/shardar é trabalho real, não vem de graça.

### Mitigação de riscos

- **Volume de `raw_event`:** retenção de 90 dias online é decidida no schema; job de arquivamento para S3/blob roda mensalmente. Documentar como reidratar dados arquivados em caso de necessidade.
- **Lentidão futura de agregação:** monitorar `pg_stat_statements` desde o dia 1; criar alerta se P95 de queries de dashboard passar de 500ms.
- **Migração futura para Timescale:** estruturar as colunas de tempo (`finished_at`, `created_at`) com índices apropriados, e nomear as tabelas de forma compatível com hypertables (já está feito em [06-data-model.md](../06-data-model.md)). A migração se torna `SELECT create_hypertable(...)` quando/se precisarmos.
- **Backup:** `pg_dump` diário + WAL archiving a partir da Fase 2.

## Notas de implementação

**Conexão e pool:**

- Lib: `github.com/jackc/pgx/v5/pgxpool` (alinhado com [ADR 0001](0001-stack-go-angular.md)).
- Pool size: começar com `max_conns=20` por instância da API; ajustar com base em métricas.
- Statement timeout: 5s para queries de leitura no path do dashboard; sem timeout para jobs de recálculo (rodam em workers).

**Schemas:**

```sql
CREATE SCHEMA platform;   -- tenant, team, project, source_instance, etc.
CREATE SCHEMA raw;        -- raw_event
CREATE SCHEMA metrics;    -- metric_daily, metric_window, metric_monthly_snapshot
```

**Migrations:**

```
backend/migrations/
├── 0001_create_platform_schema.up.sql
├── 0001_create_platform_schema.down.sql
├── 0002_create_raw_event.up.sql
├── 0002_create_raw_event.down.sql
└── ...
```

**Versão alvo (maio/2026):** PostgreSQL **18** (18.4 lançado 2026-05-11). Justificativa: melhorias em I/O assíncrono, OAuth nativo para autenticação, e UUIDv7 nativo (`uuidv7()`) — relevante para nossos PKs ordenáveis em tempo.

**Container de desenvolvimento:**

```yaml
# docker-compose.yml (trecho)
services:
  postgres:
    image: postgres:18-alpine
    environment:
      POSTGRES_DB: dora
      POSTGRES_USER: dora
      POSTGRES_PASSWORD: dora
    ports: ["5432:5432"]
    volumes:
      - pgdata:/var/lib/postgresql/data
```

**Critérios para revisitar esta decisão (revisar caso algum gatilhe):**

- `raw_event` ultrapassar 50M linhas → considerar TimescaleDB ou particionamento manual.
- Queries de dashboard com P95 > 500ms sustentado → otimizar índices/materialized views antes; se não bastar, particionar.
- Mais de 200 projetos monitorados → revisitar arquitetura de coleta antes de revisitar banco.

## Referências

- [docs/05-architecture.md § Decisão D2](../05-architecture.md#decisão-d2--banco)
- [docs/06-data-model.md](../06-data-model.md) — DDL completo
- [docs/07-roadmap.md § Fase 0](../07-roadmap.md#fase-0--funda%C3%A7%C3%A3o-1-sprint)
- [ADR 0001 — Stack Go + Angular](0001-stack-go-angular.md)
