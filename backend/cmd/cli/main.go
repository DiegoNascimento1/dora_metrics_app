// Command cli é a ferramenta administrativa de linha de comando da plataforma
// DORA Metrics. Permite cadastrar tenant, source-instance e project antes
// dos endpoints REST de admin existirem (Fase 4).
//
// Uso:
//
//	cli tenant add --slug acme --name "ACME Corp"
//	cli source-instance add --tenant acme --kind gitlab \
//	    --base-url https://gitlab.com --name gitlab-prod \
//	    --auth-ref GITLAB_TOKEN
//	cli project add --tenant acme --source gitlab-prod \
//	    --external-id 123 --path acme/api
//	cli project list --tenant acme
//	cli collect now --project <uuid>     # enfileira coleta imediata
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dora-metrics-app/backend/internal/calculator"
	"github.com/dora-metrics-app/backend/internal/collector"
	"github.com/dora-metrics-app/backend/internal/config"
	"github.com/dora-metrics-app/backend/internal/identities"
	"github.com/dora-metrics-app/backend/internal/storage"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd, sub, rest := splitArgs(os.Args[1:])

	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		die("load config: %v", err)
	}

	pool, err := storage.NewPool(ctx, cfg.Database)
	if err != nil {
		die("connect database: %v", err)
	}
	defer pool.Close()

	q := queries.New(pool.Pool)

	switch cmd {
	case "tenant":
		switch sub {
		case "add":
			tenantAdd(ctx, q, rest)
		case "list":
			tenantList(ctx, q)
		default:
			die("unknown subcommand: tenant %s", sub)
		}
	case "source-instance":
		switch sub {
		case "add":
			sourceInstanceAdd(ctx, q, rest)
		case "list":
			sourceInstanceList(ctx, q, rest)
		default:
			die("unknown subcommand: source-instance %s", sub)
		}
	case "project":
		switch sub {
		case "add":
			projectAdd(ctx, q, rest)
		case "list":
			projectList(ctx, q)
		default:
			die("unknown subcommand: project %s", sub)
		}
	case "collect":
		if sub != "now" {
			die("unknown subcommand: collect %s", sub)
		}
		collectNow(ctx, q, cfg, rest)
	case "compute":
		if sub != "now" {
			die("unknown subcommand: compute %s", sub)
		}
		computeNow(ctx, q, cfg, rest)
	case "thresholds":
		switch sub {
		case "get":
			thresholdsGet(ctx, q, rest)
		case "set":
			thresholdsSet(ctx, q, rest)
		case "defaults":
			emitJSON(calculator.DefaultThresholds())
		default:
			die("unknown subcommand: thresholds %s", sub)
		}
	case "people":
		switch sub {
		case "backfill":
			peopleBackfill(ctx, q, rest)
		case "list-unlinked":
			peopleListUnlinked(ctx, q, rest)
		case "create":
			peopleCreate(ctx, q, rest)
		case "link":
			peopleLink(ctx, q, rest)
		case "automatch":
			peopleAutomatch(ctx, q, rest)
		default:
			die("unknown subcommand: people %s", sub)
		}
	default:
		usage()
		os.Exit(1)
	}
}

// ---- tenant ----

func tenantAdd(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("tenant add", flag.ExitOnError)
	slug := fs.String("slug", "", "tenant slug (unique, kebab-case)")
	name := fs.String("name", "", "human-friendly name")
	_ = fs.Parse(args)

	if *slug == "" || *name == "" {
		die("tenant add: --slug and --name required")
	}

	row, err := q.CreateTenant(ctx, queries.CreateTenantParams{
		Slug: *slug,
		Name: *name,
	})
	if err != nil {
		die("create tenant: %v", err)
	}
	emitJSON(row)
}

func tenantList(ctx context.Context, q *queries.Queries) {
	rows, err := q.ListTenants(ctx)
	if err != nil {
		die("list tenants: %v", err)
	}
	emitJSON(rows)
}

// ---- source-instance ----

func sourceInstanceAdd(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("source-instance add", flag.ExitOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug")
	kind := fs.String("kind", "", "gitlab | jira")
	baseURL := fs.String("base-url", "", "API base URL (e.g. https://gitlab.com)")
	name := fs.String("name", "", "display name")
	authRef := fs.String("auth-ref", "", "env var name holding the token")
	_ = fs.Parse(args)

	if *tenantSlug == "" || *kind == "" || *baseURL == "" || *name == "" || *authRef == "" {
		die("source-instance add: --tenant, --kind, --base-url, --name and --auth-ref required")
	}
	if *kind != "gitlab" && *kind != "jira" {
		die("source-instance add: --kind must be gitlab or jira")
	}

	tenant, err := q.GetTenantBySlug(ctx, *tenantSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			die("tenant not found: %s", *tenantSlug)
		}
		die("get tenant: %v", err)
	}

	row, err := q.CreateSourceInstance(ctx, queries.CreateSourceInstanceParams{
		TenantID:    tenant.ID,
		Kind:        *kind,
		BaseUrl:     *baseURL,
		DisplayName: *name,
		AuthRef:     *authRef,
	})
	if err != nil {
		die("create source-instance: %v", err)
	}
	emitJSON(row)
}

func sourceInstanceList(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("source-instance list", flag.ExitOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug")
	_ = fs.Parse(args)
	if *tenantSlug == "" {
		die("source-instance list: --tenant required")
	}
	tenant, err := q.GetTenantBySlug(ctx, *tenantSlug)
	if err != nil {
		die("get tenant: %v", err)
	}
	rows, err := q.ListSourceInstancesByTenant(ctx, tenant.ID)
	if err != nil {
		die("list source-instances: %v", err)
	}
	emitJSON(rows)
}

// ---- project ----

func projectAdd(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("project add", flag.ExitOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug")
	sourceName := fs.String("source", "", "source-instance display name")
	externalID := fs.String("external-id", "", "GitLab project ID (numeric or path)")
	pathNS := fs.String("path", "", "path with namespace, e.g. group/repo")
	branch := fs.String("default-branch", "main", "default branch")
	prodPattern := fs.String("prod-env-pattern", `^prod(uction)?(-[a-z0-9-]+)?$`,
		"regex matching production environment names")
	jql := fs.String("incident-jql", `issuetype = "Incident"`, "JQL for incidents")
	_ = fs.Parse(args)

	if *tenantSlug == "" || *sourceName == "" || *externalID == "" || *pathNS == "" {
		die("project add: --tenant, --source, --external-id and --path required")
	}

	tenant, err := q.GetTenantBySlug(ctx, *tenantSlug)
	if err != nil {
		die("get tenant: %v", err)
	}

	sources, err := q.ListSourceInstancesByTenant(ctx, tenant.ID)
	if err != nil {
		die("list sources: %v", err)
	}
	var source *queries.PlatformSourceInstance
	for i := range sources {
		if sources[i].DisplayName == *sourceName {
			source = &sources[i]
			break
		}
	}
	if source == nil {
		die("source-instance not found in tenant %s: %s", *tenantSlug, *sourceName)
	}

	row, err := q.CreateProject(ctx, queries.CreateProjectParams{
		TenantID:             tenant.ID,
		SourceInstanceID:     source.ID,
		ExternalID:           *externalID,
		PathWithNamespace:    *pathNS,
		DefaultBranch:        *branch,
		ProductionEnvPattern: *prodPattern,
		IncidentJql:          *jql,
		JiraProjectKeys:      []string{},
	})
	if err != nil {
		die("create project: %v", err)
	}
	emitJSON(row)
}

func projectList(ctx context.Context, q *queries.Queries) {
	rows, err := q.ListProjects(ctx)
	if err != nil {
		die("list projects: %v", err)
	}
	emitJSON(rows)
}

// ---- collect now ----

func collectNow(ctx context.Context, q *queries.Queries, cfg config.Config, args []string) {
	fs := flag.NewFlagSet("collect now", flag.ExitOnError)
	projectIDStr := fs.String("project", "", "project UUID")
	_ = fs.Parse(args)

	if *projectIDStr == "" {
		die("collect now: --project required")
	}

	projectID, err := uuid.Parse(*projectIDStr)
	if err != nil {
		die("invalid uuid: %v", err)
	}

	if _, err := q.GetProject(ctx, projectID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			die("project not found")
		}
		die("get project: %v", err)
	}

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Worker.RedisAddr})
	defer client.Close()

	task, err := collector.NewCollectGitlabTask(projectID)
	if err != nil {
		die("build task: %v", err)
	}

	info, err := client.EnqueueContext(ctx, task)
	if err != nil {
		die("enqueue: %v", err)
	}
	emitJSON(map[string]string{
		"task_id":  info.ID,
		"queue":    info.Queue,
		"state":    info.State.String(),
	})
}

// ---- thresholds ----

func thresholdsGet(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("thresholds get", flag.ExitOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug")
	_ = fs.Parse(args)
	if *tenantSlug == "" {
		die("thresholds get: --tenant required")
	}
	tenant, err := q.GetTenantBySlug(ctx, *tenantSlug)
	if err != nil {
		die("get tenant: %v", err)
	}
	row, err := q.GetClassificationThreshold(ctx, tenant.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		fmt.Fprintln(os.Stderr, "no override for tenant; using defaults")
		emitJSON(calculator.DefaultThresholds())
		return
	}
	if err != nil {
		die("get thresholds: %v", err)
	}
	t, err := calculator.FromJSON(row.Config)
	if err != nil {
		die("decode thresholds: %v", err)
	}
	emitJSON(t)
}

func thresholdsSet(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("thresholds set", flag.ExitOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug")
	configFile := fs.String("file", "-", "JSON file with thresholds (or - for stdin)")
	_ = fs.Parse(args)
	if *tenantSlug == "" {
		die("thresholds set: --tenant required")
	}

	var raw []byte
	var err error
	if *configFile == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(*configFile)
	}
	if err != nil {
		die("read config: %v", err)
	}

	// Valida o JSON contra a struct antes de gravar.
	if _, err := calculator.FromJSON(raw); err != nil {
		die("invalid thresholds JSON: %v", err)
	}

	tenant, err := q.GetTenantBySlug(ctx, *tenantSlug)
	if err != nil {
		die("get tenant: %v", err)
	}

	row, err := q.UpsertClassificationThreshold(ctx,
		queries.UpsertClassificationThresholdParams{
			TenantID: tenant.ID,
			Config:   raw,
		})
	if err != nil {
		die("upsert thresholds: %v", err)
	}
	emitJSON(row)
}

// ---- people ----

func peopleBackfill(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("people backfill", flag.ExitOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug")
	_ = fs.Parse(args)
	if *tenantSlug == "" {
		die("people backfill: --tenant required")
	}

	tenant, err := q.GetTenantBySlug(ctx, *tenantSlug)
	if err != nil {
		die("get tenant: %v", err)
	}

	usernames, err := q.ListGitlabUsernamesFromEvents(ctx, tenant.ID)
	if err != nil {
		die("list usernames: %v", err)
	}

	created := 0
	for _, u := range usernames {
		_, err := q.UpsertPersonIdentity(ctx, queries.UpsertPersonIdentityParams{
			TenantID:         tenant.ID,
			SourceInstanceID: pgtype.UUID{}, // backfill sintético — sem source vinculado
			Kind:             "gitlab",
			ExternalID:       nil,
			ExternalUsername: u,
			ExternalEmail:    nil,
		})
		if err != nil {
			die("upsert identity %s: %v", u, err)
		}
		created++
	}

	emitJSON(map[string]any{
		"tenant":             tenant.Slug,
		"usernames_scanned":  len(usernames),
		"identities_upserted": created,
	})
}

func peopleListUnlinked(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("people list-unlinked", flag.ExitOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug")
	_ = fs.Parse(args)
	if *tenantSlug == "" {
		die("people list-unlinked: --tenant required")
	}
	tenant, err := q.GetTenantBySlug(ctx, *tenantSlug)
	if err != nil {
		die("get tenant: %v", err)
	}
	rows, err := q.ListUnlinkedIdentities(ctx, tenant.ID)
	if err != nil {
		die("list unlinked: %v", err)
	}
	emitJSON(rows)
}

func peopleCreate(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("people create", flag.ExitOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug")
	name := fs.String("name", "", "display name")
	email := fs.String("email", "", "primary email (used by auto-match)")
	avatar := fs.String("avatar", "", "avatar URL")
	_ = fs.Parse(args)

	if *tenantSlug == "" || *name == "" {
		die("people create: --tenant and --name required")
	}
	tenant, err := q.GetTenantBySlug(ctx, *tenantSlug)
	if err != nil {
		die("get tenant: %v", err)
	}

	row, err := q.CreatePerson(ctx, queries.CreatePersonParams{
		TenantID:     tenant.ID,
		DisplayName:  *name,
		PrimaryEmail: strPtr(*email),
		AvatarUrl:    strPtr(*avatar),
	})
	if err != nil {
		die("create person: %v", err)
	}
	emitJSON(row)
}

func peopleLink(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("people link", flag.ExitOnError)
	identityIDStr := fs.String("identity", "", "person_identity UUID")
	personIDStr := fs.String("person", "", "person UUID")
	by := fs.String("by", "cli", "linked_by (audit)")
	_ = fs.Parse(args)

	if *identityIDStr == "" || *personIDStr == "" {
		die("people link: --identity and --person required")
	}

	identityID, err := uuid.Parse(*identityIDStr)
	if err != nil {
		die("invalid identity uuid: %v", err)
	}
	personID, err := uuid.Parse(*personIDStr)
	if err != nil {
		die("invalid person uuid: %v", err)
	}

	row, err := q.LinkIdentityToPerson(ctx, queries.LinkIdentityToPersonParams{
		IdentityID: identityID,
		PersonID:   pgtype.UUID{Bytes: personID, Valid: true},
		LinkedBy:   by,
	})
	if err != nil {
		die("link identity: %v", err)
	}
	emitJSON(row)
}

func peopleAutomatch(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("people automatch", flag.ExitOnError)
	tenantSlug := fs.String("tenant", "", "tenant slug")
	_ = fs.Parse(args)
	if *tenantSlug == "" {
		die("people automatch: --tenant required")
	}
	tenant, err := q.GetTenantBySlug(ctx, *tenantSlug)
	if err != nil {
		die("get tenant: %v", err)
	}

	rows, err := q.ListUnlinkedIdentities(ctx, tenant.ID)
	if err != nil {
		die("list unlinked: %v", err)
	}

	src := make([]identities.Identity, 0, len(rows))
	for _, r := range rows {
		var email string
		if r.ExternalEmail != nil {
			email = *r.ExternalEmail
		}
		src = append(src, identities.Identity{
			ID:               r.ID,
			Kind:             r.Kind,
			ExternalUsername: r.ExternalUsername,
			ExternalEmail:    email,
		})
	}

	suggestions := identities.Match(src)
	emitJSON(map[string]any{
		"tenant":      tenant.Slug,
		"unlinked":    len(src),
		"suggestions": suggestions,
	})
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---- compute now ----

func computeNow(ctx context.Context, q *queries.Queries, cfg config.Config, args []string) {
	fs := flag.NewFlagSet("compute now", flag.ExitOnError)
	projectIDStr := fs.String("project", "", "project UUID")
	windowDays := fs.Int("window", 30, "window in days (7, 30, 90)")
	_ = fs.Parse(args)

	if *projectIDStr == "" {
		die("compute now: --project required")
	}

	projectID, err := uuid.Parse(*projectIDStr)
	if err != nil {
		die("invalid uuid: %v", err)
	}

	if _, err := q.GetProject(ctx, projectID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			die("project not found")
		}
		die("get project: %v", err)
	}

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.Worker.RedisAddr})
	defer client.Close()

	task, err := collector.NewComputeMetricWindowTask(projectID, *windowDays)
	if err != nil {
		die("build task: %v", err)
	}

	info, err := client.EnqueueContext(ctx, task)
	if err != nil {
		die("enqueue: %v", err)
	}
	emitJSON(map[string]string{
		"task_id": info.ID,
		"queue":   info.Queue,
		"state":   info.State.String(),
	})
}

// ---- helpers ----

func splitArgs(args []string) (cmd, sub string, rest []string) {
	if len(args) == 0 {
		return "", "", nil
	}
	cmd = args[0]
	if len(args) >= 2 {
		sub = args[1]
	}
	if len(args) >= 3 {
		rest = args[2:]
	}
	return
}

func emitJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func die(format string, args ...any) {
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}
	fmt.Fprintf(os.Stderr, "cli: "+format, args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `DORA Metrics CLI

Usage:
  cli tenant add --slug X --name Y
  cli tenant list
  cli source-instance add --tenant X --kind gitlab --base-url URL --name N --auth-ref ENV_VAR
  cli source-instance list --tenant X
  cli project add --tenant X --source N --external-id ID --path group/repo
  cli project list
  cli collect now --project UUID
  cli compute now --project UUID --window 30
  cli thresholds get --tenant X
  cli thresholds set --tenant X [--file thresholds.json | - for stdin]
  cli thresholds defaults
  cli people backfill --tenant X
  cli people list-unlinked --tenant X
  cli people create --tenant X --name "Alice Doe" [--email alice@acme.com]
  cli people link --identity UUID --person UUID
  cli people automatch --tenant X`)
}
