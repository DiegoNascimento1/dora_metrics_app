# Grafana — Dashboards DORA

Painel exemplo que consome as métricas Prometheus emitidas pelo backend
(`dora_http_*`, `dora_asynq_*`).

## Subir Grafana local

```bash
docker compose -f ops/grafana/docker-compose.yml up -d
# Grafana: http://localhost:3001  (admin / admin)
```

O compose deste diretório sobe:

- Prometheus (scrape em `api:8080/metrics` e `worker:9090/metrics`)
- Grafana com provisioning automático do dashboard `dora-overview`

> Depende dos serviços `api` e `worker` do `docker-compose.yml` principal
> estarem em pé. Use:
>
> ```bash
> docker compose --profile full up -d  # raiz do repo
> docker compose -f ops/grafana/docker-compose.yml up -d
> ```

## Painéis do dashboard `dora-overview`

1. **HTTP P95 por rota** — `histogram_quantile(0.95, sum by (le, route) (rate(dora_http_request_duration_seconds_bucket[5m])))`
2. **HTTP throughput por status** — `sum by (status) (rate(dora_http_requests_total[5m]))`
3. **Asynq throughput por tipo** — `sum by (type) (rate(dora_asynq_tasks_total[5m]))`
4. **Asynq error rate** — `sum by (type) (rate(dora_asynq_tasks_total{status="error"}[5m])) / sum by (type) (rate(dora_asynq_tasks_total[5m]))`
5. **Latência média de tarefa** — `avg by (type) (rate(dora_asynq_task_duration_seconds_sum[5m]) / rate(dora_asynq_task_duration_seconds_count[5m]))`

## OpenTelemetry tracing

Para habilitar tracing distribuído, configure no ambiente do `api` e
`worker`:

```env
OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
OTEL_EXPORTER_OTLP_INSECURE=true
OTEL_SERVICE_VERSION=$(git rev-parse --short HEAD)
```

Sem essas variáveis, OTel fica em no-op (zero overhead).
