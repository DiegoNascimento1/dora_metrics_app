// Package calculator implementa o cálculo das 4 métricas DORA
// a partir dos dados consolidados no storage.
//
// Documentação detalhada em ../../../docs/01-dora-metrics.md
// e ../../../docs/06-data-model.md (queries canônicas).
//
// As funções principais (a serem implementadas na Fase 2):
//
//   - LeadTime(ctx, projectID, window) → mediana em segundos
//   - DeploymentFrequency(ctx, projectID, window) → deploys/dia
//   - ChangeFailureRate(ctx, projectID, window) → 0.0 a 1.0
//   - MTTR(ctx, projectID, window) → média em segundos
//   - Classify(metrics, thresholds) → "elite" | "high" | "medium" | "low"
package calculator
