// Package secret abstrai o acesso a credenciais sensíveis.
//
// No MVP usamos apenas o EnvProvider (variáveis de ambiente). Em produção,
// trocar para um provider que busque em vault (HashiCorp Vault, AWS Secrets
// Manager, Azure Key Vault) sem mudar o restante do código.
package secret

import (
	"context"
	"errors"
	"os"
)

// ErrNotFound é retornado quando o segredo não existe no provider.
var ErrNotFound = errors.New("secret not found")

// Provider é a interface implementada por cada backend de secret management.
type Provider interface {
	// Get devolve o valor do segredo identificado por key.
	// Retorna ErrNotFound se o segredo não existe.
	Get(ctx context.Context, key string) (string, error)
}

// EnvProvider lê segredos das variáveis de ambiente.
// Implementação default para o MVP.
type EnvProvider struct{}

// NewEnvProvider devolve um EnvProvider.
func NewEnvProvider() *EnvProvider {
	return &EnvProvider{}
}

// Get devolve a env var de mesmo nome.
func (e *EnvProvider) Get(_ context.Context, key string) (string, error) {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return "", ErrNotFound
	}
	return val, nil
}

// New constrói um Provider a partir do nome configurado.
//
// Valores aceitos:
//   - "env"   → EnvProvider (default)
//   - "vault" → VaultProvider (HashiCorp Vault KVv2). Requer VAULT_ADDR
//               + VAULT_TOKEN. Detalhes em vault.go.
//   - "aws-secrets-manager", "azure-key-vault" → não implementados ainda
//     (retorna erro). PRs bem-vindos.
func New(kind string) (Provider, error) {
	switch kind {
	case "", "env":
		return NewEnvProvider(), nil
	case "vault":
		return NewVaultProvider()
	case "aws-secrets-manager":
		return NewAWSSecretsManagerProvider()
	case "azure-key-vault":
		return NewAzureKeyVaultProvider()
	default:
		return nil, errors.New("unknown secret provider: " + kind)
	}
}
