// Tracing: OpenTelemetry. Inicializa um TracerProvider com exporter OTLP
// gRPC. Se `OTEL_EXPORTER_OTLP_ENDPOINT` não estiver definido, vira
// no-op silencioso — usar OTel só em ambientes que têm coletor rodando.
//
// Como usar:
//
//	shutdown, err := observability.InitTracing(ctx, "api")
//	defer shutdown(ctx) // dispara flush dos spans pendentes
package observability

import (
	"context"
	"errors"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// ShutdownFunc encerra o tracer provider, fazendo flush pendente.
type ShutdownFunc func(context.Context) error

// noopShutdown é devolvido quando OTel é desligado (env não setada).
var noopShutdown ShutdownFunc = func(_ context.Context) error { return nil }

// InitTracing inicializa o OTel TracerProvider global. Devolve uma função
// de shutdown idempotente. `serviceName` aparece como `service.name` em
// todos os spans (ex: "api", "worker", "mcp-server").
//
// Variáveis lidas:
//   - OTEL_EXPORTER_OTLP_ENDPOINT (ex: "otel-collector:4317"). Vazio = no-op.
//   - OTEL_EXPORTER_OTLP_INSECURE = "true" para gRPC sem TLS.
//   - OTEL_SERVICE_VERSION (opcional, default "dev").
func InitTracing(ctx context.Context, serviceName string) (ShutdownFunc, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		return noopShutdown, nil
	}

	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
	if os.Getenv("OTEL_EXPORTER_OTLP_INSECURE") == "true" {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return noopShutdown, err
	}

	version := os.Getenv("OTEL_SERVICE_VERSION")
	if version == "" {
		version = "dev"
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return noopShutdown, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		return errors.Join(tp.ForceFlush(ctx), tp.Shutdown(ctx))
	}, nil
}
