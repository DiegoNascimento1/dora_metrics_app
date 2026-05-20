# DORA Metrics App

Plataforma de DORA Metrics integrando GitLab (API) e Jira (MCP).
Veja o estudo completo em [docs/](docs/README.md) e o contexto em [CLAUDE.md](CLAUDE.md).

## Stack (maio/2026)

- **Backend:** Go 1.26 — chi, pgx/v5, sqlc, asynq
- **Frontend:** Angular 21 (standalone, `latest` no npm; Angular 22 em RC) — Angular Material, ng2-charts, signals nativos — TypeScript 5.9 — Node 24 LTS
- **Banco:** PostgreSQL 18
- **Fila:** Redis (asynq)

## Requisitos

- Docker + Docker Compose
- Go 1.26+
- Node 24+ (npm 10+)
- `make`

Opcionais:
- `golangci-lint` 1.62+
- `sqlc` 1.27+
- `golang-migrate`
- `openapi-typescript`

## Quick start

```bash
cp .env.example .env
make up                    # sobe Postgres + Redis
make be-migrate            # aplica schema
make be-sqlc               # gera código de DB

# Em um terminal:
make be-run                # roda API em :8080

# Em outro:
make fe-install
make fe-run                # roda Angular em :4200
```

Para subir tudo em containers:

```bash
make up-full
```

### Sem `make`, Go ou `migrate` instalados? (Windows / onboarding rápido)

Só com Docker dá pra subir e validar o schema sem instalar a stack toda:

```powershell
# Sobe Postgres + Redis
docker compose up -d postgres redis

# Aplica migrations via container oficial (sem precisar do binário `migrate`)
docker run --rm `
  --network dora-metrics_default `
  -v "${PWD}/backend/migrations:/migrations" `
  migrate/migrate:v4.18.1 `
  -path=/migrations `
  -database "postgres://dora:dora@dora-metrics-postgres-1:5432/dora?sslmode=disable" `
  up

# Conferir schema
docker exec dora-metrics-postgres-1 psql -U dora -d dora -c "\dt platform.*"
```

> Em Git Bash no Windows, prefixe `MSYS_NO_PATHCONV=1` no `docker run` acima
> pra evitar que `/migrations` seja convertido para um caminho Windows.

Para instalar a stack completa:

```powershell
winget install GoLang.Go
winget install OpenJS.NodeJS.LTS    # Node 24
winget install GnuWin32.Make
# migrate via go install após Go pronto:
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

## Layout

```
.
├── backend/        Go: api + worker + migrations
├── frontend/       Angular 22
├── docs/           Estudo profundo + ADRs
├── openapi.yaml    Contrato API (gera types pro front)
├── docker-compose.yml
└── Makefile
```

## Próximos passos

Ver [docs/07-roadmap.md](docs/07-roadmap.md). Estamos saindo da Fase 0; próximo: Fase 1 — MVP de coleta.

## Documentação

| Doc | Conteúdo |
|---|---|
| [CLAUDE.md](CLAUDE.md) | Contexto do projeto carregado pelas sessões Claude Code |
| [docs/](docs/README.md) | Estudo profundo (DORA, MCP, GitLab, Jira, arquitetura) |
| [docs/adr/](docs/adr/) | Decisões arquiteturais registradas |
