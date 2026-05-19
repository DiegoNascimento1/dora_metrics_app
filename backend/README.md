# Backend — Go 1.26

API + worker da plataforma DORA Metrics.

## Estrutura

```
backend/
├── cmd/
│   ├── api/           # servidor HTTP (chi)
│   └── worker/        # workers asynq
├── internal/
│   ├── api/           # handlers, middleware
│   ├── calculator/    # cálculo das 4 métricas DORA
│   ├── collector/
│   │   ├── gitlab/    # cliente GitLab + handlers de webhook
│   │   └── jira/      # cliente Jira (MCP + REST fallback)
│   ├── config/        # carregamento de configuração
│   ├── secret/        # abstração de secret provider
│   └── storage/
│       ├── sql/queries/  # arquivos .sql (input sqlc)
│       └── queries/      # código Go gerado (gitignore)
├── migrations/        # *.up.sql / *.down.sql
└── sqlc.yaml
```

## Comandos

Ver o `Makefile`:

```bash
make build         # api + worker em bin/
make run           # roda api
make test          # tests com race + cover
make lint          # golangci-lint
make migrate-up    # aplica migrations
make sqlc          # gera código DB
```

## Convenções

- Logs estruturados via `zerolog`.
- Erros via `errors.New` / `fmt.Errorf` com `%w` para wrapping.
- Contexto propagado em todas as chamadas externas e queries.
- Toda função pública documentada em godoc.
