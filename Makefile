.PHONY: help up down logs ps reset \
        be-build be-run be-test be-lint be-fmt be-migrate be-sqlc \
        fe-install fe-run fe-build fe-test fe-lint fe-gen-types \
        ci

# Default target
help:
	@echo "DORA Metrics App — comandos"
	@echo ""
	@echo "  Infra (Docker)"
	@echo "    up              sobe postgres + redis (dev local)"
	@echo "    up-full         sobe tudo: pg + redis + api + worker + web"
	@echo "    down            derruba containers"
	@echo "    logs            tail dos logs"
	@echo "    reset           DESTROI volumes (apaga dados)"
	@echo ""
	@echo "  Backend (Go)"
	@echo "    be-build        compila api + worker"
	@echo "    be-run          roda api localmente"
	@echo "    be-test         go test ./..."
	@echo "    be-lint         golangci-lint run"
	@echo "    be-fmt          gofmt + goimports"
	@echo "    be-migrate      aplica migrations (up)"
	@echo "    be-sqlc         gera código sqlc"
	@echo ""
	@echo "  Frontend (Angular 22)"
	@echo "    fe-install      npm install"
	@echo "    fe-run          ng serve"
	@echo "    fe-build        ng build --configuration production"
	@echo "    fe-test         ng test"
	@echo "    fe-lint         ng lint"
	@echo "    fe-gen-types    gera tipos do openapi.yaml"
	@echo ""
	@echo "  CI"
	@echo "    ci              roda lint + test em todos os módulos"

# ---------- Infra ----------
up:
	docker compose up -d postgres redis

up-full:
	docker compose --profile full up -d

down:
	docker compose down

logs:
	docker compose logs -f --tail=200

ps:
	docker compose ps

reset:
	docker compose down -v

# ---------- Backend ----------
be-build:
	$(MAKE) -C backend build

be-run:
	$(MAKE) -C backend run

be-test:
	$(MAKE) -C backend test

be-lint:
	$(MAKE) -C backend lint

be-fmt:
	$(MAKE) -C backend fmt

be-migrate:
	$(MAKE) -C backend migrate-up

be-sqlc:
	$(MAKE) -C backend sqlc

# ---------- Frontend ----------
fe-install:
	$(MAKE) -C frontend install

fe-run:
	$(MAKE) -C frontend run

fe-build:
	$(MAKE) -C frontend build

fe-test:
	$(MAKE) -C frontend test

fe-lint:
	$(MAKE) -C frontend lint

fe-gen-types:
	$(MAKE) -C frontend gen-types

# ---------- CI ----------
ci: be-lint be-test fe-lint fe-test
