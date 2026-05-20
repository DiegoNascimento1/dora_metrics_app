# Frontend — Angular 21

Dashboard da plataforma DORA Metrics.

## Stack

- Angular 21 (standalone components, signals, zoneless change detection)
- Angular Material (tema `azure-blue`)
- Chart.js + ng2-charts
- Signals nativos do Angular (NgRx signals será adicionado quando houver versão compatível com Angular 21+)
- TypeScript 5.9
- Node 24 LTS no toolchain

## Comandos

```bash
make install        # npm ci
make run            # ng serve em :4200
make build          # build de produção
make test           # karma + jasmine headless
make lint           # angular-eslint
make gen-types      # gera types do ../openapi.yaml
```

## Estrutura

```
frontend/src/app/
├── core/               # api client, auth, interceptors
│   └── api/
├── features/           # rotas/features
│   ├── dashboard/
│   └── projects/
└── shared/             # componentes reutilizáveis
```

## Tipos da API

Manuais em `core/api/api.types.ts` para o MVP. CI executa `gen-types` que substitui por `core/api/generated/api-types.ts` (gerado de `openapi.yaml`).
