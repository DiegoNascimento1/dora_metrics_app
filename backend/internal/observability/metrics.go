// Package observability provê middlewares para emitir métricas Prometheus
// das chamadas HTTP (chi) e dos handlers asynq.
//
// Exposição: handler() devolve um http.Handler que pode ser montado em
// /metrics em qualquer servidor HTTP (API ou worker).
package observability

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Namespace usado pra todas as métricas — facilita filtrar no Prometheus
// (`dora_*`).
const namespace = "dora"

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total de requisições HTTP por route + method + status code.",
		},
		[]string{"route", "method", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "Latência das requisições HTTP em segundos.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"route", "method"},
	)

	asynqTasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "asynq",
			Name:      "tasks_total",
			Help:      "Tasks asynq processadas, contadas por tipo + status (success|error|skip_retry).",
		},
		[]string{"type", "status"},
	)

	asynqTaskDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "asynq",
			Name:      "task_duration_seconds",
			Help:      "Latência dos handlers asynq em segundos.",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 300},
		},
		[]string{"type"},
	)
)

var registered = false

// Register registra todos os collectors. Idempotente — chamar mais de uma
// vez no mesmo processo só faz no-op.
func Register() {
	if registered {
		return
	}
	prometheus.MustRegister(httpRequestsTotal, httpRequestDuration, asynqTasksTotal, asynqTaskDuration)
	registered = true
}

// Handler devolve o handler /metrics pronto pra montar em qualquer mux.
func Handler() http.Handler {
	return promhttp.Handler()
}

// HTTPMiddleware é compatível com chi (signature func(http.Handler) http.Handler).
// Usa o RoutePattern do chi pra agrupar request por padrão de rota em vez
// de instância concreta (evita cardinalidade explosiva com path params).
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		// chi.RoutePattern só está disponível após o router casar a rota,
		// o que acontece quando o handler retorna; recuperamos do contexto.
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unknown"
		}
		method := r.Method
		status := strconv.Itoa(rw.status)

		httpRequestsTotal.WithLabelValues(route, method, status).Inc()
		httpRequestDuration.WithLabelValues(route, method).Observe(time.Since(start).Seconds())
	})
}

// AsynqMiddleware embrulha um asynq.Handler com observabilidade.
// Conta success/error/skip_retry como status separados pra alertas
// reagirem só ao que realmente é problema.
func AsynqMiddleware(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		start := time.Now()
		err := next.ProcessTask(ctx, t)
		asynqTaskDuration.WithLabelValues(t.Type()).Observe(time.Since(start).Seconds())

		switch {
		case err == nil:
			asynqTasksTotal.WithLabelValues(t.Type(), "success").Inc()
		case errors.Is(err, asynq.SkipRetry):
			asynqTasksTotal.WithLabelValues(t.Type(), "skip_retry").Inc()
		default:
			asynqTasksTotal.WithLabelValues(t.Type(), "error").Inc()
		}
		return err
	})
}

// statusRecorder embrulha http.ResponseWriter pra capturar o status code
// real (chi não expõe isso por default).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
