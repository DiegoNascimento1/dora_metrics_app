// CLI subcomandos para gerenciar segredos:
//
//   cli secrets check                              # valida que todos auth_ref
//                                                   das source-instances
//                                                   resolvem no Provider atual.
//   cli secrets rotate --source NAME --new-ref X   # aponta a source-instance
//                                                   para um novo nome de
//                                                   variável/segredo.
//
// Política de rotação: o admin deve (1) provisionar o novo segredo no
// backend (env, Vault, AWS Secrets Manager), (2) rodar `secrets rotate`
// apontando a source-instance pro novo ref, (3) verificar com
// `secrets check` que o lookup funciona, (4) só depois revogar o
// segredo antigo. O comando NÃO faz lookup do valor do segredo no
// runtime — só atualiza o ponteiro (auth_ref).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/dora-metrics-app/backend/internal/config"
	"github.com/dora-metrics-app/backend/internal/secret"
	"github.com/dora-metrics-app/backend/internal/storage/queries"
)

// secretsCheck percorre todas as source-instance e tenta resolver o
// auth_ref via provider configurado. Exit 1 se algum falhar.
func secretsCheck(ctx context.Context, q *queries.Queries, cfg config.Config, _ []string) {
	provider, err := secret.New(cfg.SecretProvider)
	if err != nil {
		die("init provider %q: %v", cfg.SecretProvider, err)
	}

	tenants, err := q.ListTenants(ctx)
	if err != nil {
		die("list tenants: %v", err)
	}
	if len(tenants) == 0 {
		fmt.Println("nenhum tenant cadastrado")
		return
	}

	var failures int
	for _, tnt := range tenants {
		instances, err := q.ListSourceInstancesByTenant(ctx, tnt.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tenant %s: list source-instances: %v\n", tnt.Slug, err)
			failures++
			continue
		}
		for _, si := range instances {
			if si.AuthRef == "" {
				fmt.Printf("  %s/%s: ⚠️  auth_ref vazio (sem segredo)\n", tnt.Slug, si.DisplayName)
				continue
			}
			val, err := provider.Get(ctx, si.AuthRef)
			if errors.Is(err, secret.ErrNotFound) {
				fmt.Printf("  %s/%s: ❌ %s NOT FOUND no provider %q\n", tnt.Slug, si.DisplayName, si.AuthRef, cfg.SecretProvider)
				failures++
				continue
			}
			if err != nil {
				fmt.Printf("  %s/%s: ❌ %s erro: %v\n", tnt.Slug, si.DisplayName, si.AuthRef, err)
				failures++
				continue
			}
			fmt.Printf("  %s/%s: ✅ %s OK (%d bytes)\n", tnt.Slug, si.DisplayName, si.AuthRef, len(val))
		}
	}
	if failures > 0 {
		fmt.Fprintf(os.Stderr, "\n%d falha(s) — segredos faltando ou inacessíveis\n", failures)
		os.Exit(1)
	}
	fmt.Println("\ntudo OK")
}

// secretsRotate atualiza source_instance.auth_ref para apontar a um novo
// nome de segredo. Não toca o valor do segredo em si.
func secretsRotate(ctx context.Context, q *queries.Queries, args []string) {
	fs := flag.NewFlagSet("secrets rotate", flag.ExitOnError)
	sourceName := fs.String("source", "", "nome da source-instance (unique por tenant)")
	tenant := fs.String("tenant", "", "tenant slug")
	newRef := fs.String("new-ref", "", "novo nome do segredo (env var / Vault key / AWS secret name)")
	_ = fs.Parse(args)

	if *sourceName == "" || *tenant == "" || *newRef == "" {
		die("secrets rotate: --tenant, --source e --new-ref são obrigatórios")
	}

	tnt, err := q.GetTenantBySlug(ctx, *tenant)
	if err != nil {
		die("tenant %s: %v", *tenant, err)
	}

	instances, err := q.ListSourceInstancesByTenant(ctx, tnt.ID)
	if err != nil {
		die("list source-instances: %v", err)
	}
	var target *queries.PlatformSourceInstance
	for i := range instances {
		if instances[i].DisplayName == *sourceName {
			target = &instances[i]
			break
		}
	}
	if target == nil {
		die("source-instance %q não encontrada no tenant %s", *sourceName, *tenant)
	}

	oldRef := target.AuthRef
	// UpdateSourceInstanceSecret usa COALESCE no auth_ref — preserva
	// secret_value e auth_email atuais. Aqui só rotacionamos o ponteiro.
	if _, err := q.UpdateSourceInstanceSecret(ctx, queries.UpdateSourceInstanceSecretParams{
		ID:          target.ID,
		AuthRef:     newRef,
		SecretValue: target.SecretValue,
		AuthEmail:   target.AuthEmail,
	}); err != nil {
		die("rotate auth_ref: %v", err)
	}

	fmt.Printf("✅ source-instance %s/%s rotacionada\n", *tenant, *sourceName)
	fmt.Printf("    %s → %s\n", oldRef, *newRef)
	fmt.Println("\nPróximo passo: rode 'cli secrets check' para validar.")
}
