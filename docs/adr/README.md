# Architecture Decision Records (ADR)

Documentos curtos que registram **decisões arquiteturais relevantes**, seu contexto, e suas consequências. Cada decisão importante vira um ADR numerado, imutável após "Accepted".

## Como escrever um ADR

1. Copiar [0000-template.md](0000-template.md) para `NNNN-kebab-case-titulo.md` (próximo número sequencial).
2. Preencher as seções. Manter curto — um ADR longo é sinal de que a decisão está mal-formulada.
3. Status inicial: `Proposed`. Após aprovação do time, mudar para `Accepted`.
4. ADRs nunca são editados depois de aceitos — exceto para corrigir typos. **Se a decisão mudar, crie um novo ADR que substitua o antigo**, e marque o antigo como `Superseded by [NNNN]`.

## Índice

| #    | Título                                            | Status   | Data       |
| ---- | ------------------------------------------------- | -------- | ---------- |
| 0001 | [Stack — Go (backend) + Angular (frontend)](0001-stack-go-angular.md) | Accepted | 2026-05-19 |
| 0002 | [Banco de dados — PostgreSQL puro](0002-database-postgresql.md)       | Accepted | 2026-05-19 |
| 0003 | [Modelo de identidades unificadas (GitLab ↔ Jira)](0003-unified-identity-model.md) | Accepted | 2026-05-20 |
