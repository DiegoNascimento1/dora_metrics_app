package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	"github.com/dora-metrics-app/backend/internal/calculator"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

// handleTeamMetrics retorna as 4 métricas DORA agregadas para todos os
// projetos do time. Calculadas on-the-fly (não passam por metric_window).
func (s *Server) handleTeamMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID, err := uuid.Parse(chi.URLParam(r, "teamId"))
		if err != nil {
			http.Error(w, "invalid team id", http.StatusBadRequest)
			return
		}
		windowDays := windowDays(r.URL.Query().Get("window"))
		since := time.Now().UTC().AddDate(0, 0, -windowDays)

		q := queries.New(s.db.Pool)

		team, err := q.GetTeam(r.Context(), teamID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "team not found", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Msg("get team")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		tid := pgtype.UUID{Bytes: teamID, Valid: true}
		sinceTs := pgTime(since)

		count, err := q.CountSuccessfulProductionDeploymentsForTeamInWindow(r.Context(),
			queries.CountSuccessfulProductionDeploymentsForTeamInWindowParams{
				TeamID:        tid,
				FinishedSince: sinceTs,
			},
		)
		if err != nil {
			log.Error().Err(err).Msg("team count deploys")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		df := 0.0
		if windowDays > 0 {
			df = float64(count) / float64(windowDays)
		}

		ltRow, err := q.LeadTimeMedianForTeamInWindow(r.Context(),
			queries.LeadTimeMedianForTeamInWindowParams{
				TeamID:        tid,
				FinishedSince: sinceTs,
			},
		)
		if err != nil {
			log.Error().Err(err).Msg("team lead time")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		var leadTimeMedianS *int64
		if ltRow.SampleSize > 0 {
			v := int64(coerceLT(ltRow.MedianSeconds))
			leadTimeMedianS = &v
		}

		cfrRow, err := q.ChangeFailureRateForTeamInWindow(r.Context(),
			queries.ChangeFailureRateForTeamInWindowParams{
				TeamID:        tid,
				FinishedSince: sinceTs,
			},
		)
		if err != nil {
			log.Error().Err(err).Msg("team cfr")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		var cfrFloat *float64
		if cfrRow.SampleSize > 0 {
			v := teamCoerceFloat(cfrRow.Cfr)
			cfrFloat = &v
		}

		mttrRow, err := q.MTTRMeanSecondsForTeamInWindow(r.Context(),
			queries.MTTRMeanSecondsForTeamInWindowParams{
				TeamID:        tid,
				ResolvedSince: sinceTs,
			},
		)
		if err != nil {
			log.Error().Err(err).Msg("team mttr")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		var mttrMeanS *int64
		if mttrRow.SampleSize > 0 {
			v := int64(teamCoerceFloat(mttrRow.MeanSeconds))
			mttrMeanS = &v
		}

		// Thresholds do tenant — buscamos via project sample (todos os projetos
		// do team são do mesmo tenant). Como o team já passou a constraint,
		// pegamos os thresholds desse tenant.
		thresholds := calculator.DefaultThresholds()
		if row, err := q.GetClassificationThreshold(r.Context(), team.TenantID); err == nil {
			if loaded, err := calculator.FromJSON(row.Config); err == nil {
				thresholds = loaded
			}
		}

		classification := calculator.WorstOf(
			calculator.ClassifyDeploymentFrequency(df, thresholds),
			calculator.ClassifyLeadTime(leadTimeMedianS, thresholds),
			calculator.ClassifyChangeFailureRate(cfrFloat, thresholds),
			calculator.ClassifyMTTR(mttrMeanS, thresholds),
		)

		// Reusa o mesmo DTO do project/metrics (a única coisa que muda é o
		// significado de projectId — aqui é o team id). O front é responsável
		// pela rotulagem.
		writeJSON(w, http.StatusOK, doraMetricsDTO{
			ProjectID:             teamID.String(),
			WindowDays:            windowDays,
			ComputedAt:            time.Now().UTC().Format(time.RFC3339),
			DeploymentFrequency:   df,
			LeadTimeMedianSeconds: leadTimeMedianS,
			ChangeFailureRate:     cfrFloat,
			MTTRMeanSeconds:       mttrMeanS,
			Classification:        classification,
			SampleSize:            int(count),
		})
	}
}

func (s *Server) handleTeamTimeseries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID, err := uuid.Parse(chi.URLParam(r, "teamId"))
		if err != nil {
			http.Error(w, "invalid team id", http.StatusBadRequest)
			return
		}
		windowDays := windowDays(r.URL.Query().Get("window"))
		since := time.Now().UTC().AddDate(0, 0, -windowDays)

		q := queries.New(s.db.Pool)

		if _, err := q.GetTeam(r.Context(), teamID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "team not found", http.StatusNotFound)
				return
			}
			log.Error().Err(err).Msg("get team")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		rows, err := q.DeploymentsPerDayForTeamInWindow(r.Context(),
			queries.DeploymentsPerDayForTeamInWindowParams{
				TeamID:        pgtype.UUID{Bytes: teamID, Valid: true},
				FinishedSince: pgTime(since),
			},
		)
		if err != nil {
			log.Error().Err(err).Msg("team timeseries")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Converte para o mesmo formato esperado pelo frontend
		// (DeploymentsPerDayInWindowRow), preenchendo dias sem deploy com 0.
		byDay := make(map[string]int, len(rows))
		for _, r := range rows {
			if r.Day.Valid {
				byDay[r.Day.Time.Format("2006-01-02")] = int(r.DeployCount)
			}
		}
		sinceDay := since.Truncate(24 * time.Hour)
		points := make([]timeseriesPoint, 0, windowDays)
		for i := 0; i < windowDays; i++ {
			day := sinceDay.AddDate(0, 0, i).Format("2006-01-02")
			points = append(points, timeseriesPoint{Day: day, DeployCount: byDay[day]})
		}

		writeJSON(w, http.StatusOK, timeseriesResponse{
			ProjectID:  teamID.String(),
			WindowDays: windowDays,
			Metric:     "deployment_frequency",
			Points:     points,
		})
	}
}

// teamCoerceFloat lida com o tipo interface{} do sqlc para campos COALESCE.
func teamCoerceFloat(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case pgtype.Numeric:
		if !x.Valid {
			return 0
		}
		f, err := x.Float64Value()
		if err != nil || !f.Valid {
			return 0
		}
		return f.Float64
	case []byte:
		var n pgtype.Numeric
		if err := n.Scan(string(x)); err == nil {
			f, _ := n.Float64Value()
			if f.Valid {
				return f.Float64
			}
		}
		return 0
	}
	return 0
}
