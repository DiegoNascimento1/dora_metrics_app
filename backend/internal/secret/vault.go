// VaultProvider lê segredos do HashiCorp Vault via KVv2 secret engine.
//
// Endereço e auth lidos do ambiente:
//   - VAULT_ADDR        — URL base (ex: "https://vault.internal:8200")
//   - VAULT_TOKEN       — token de autenticação (curto-prazo recomendado)
//   - VAULT_KV_MOUNT    — mount path da KVv2 engine (default "secret")
//   - VAULT_PATH_PREFIX — prefixo aplicado ao path antes de cada Get
//                        (ex: "dora-metrics/prod"). Default vazio.
//
// O cliente é um wrapper minimalista sobre HTTP — não usamos o SDK
// oficial `github.com/hashicorp/vault/api` para evitar puxar dependências
// pesadas para uma feature opt-in. Quando a operação ficar não-trivial
// (renovação de token, AppRole login, leases), migrar.
//
// Em runtime, se um Get falhar (ex: Vault inacessível), devolve o erro
// para o caller decidir — o secret.Provider não tem fallback embutido.
package secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// VaultProvider é o secret.Provider que fala com HashiCorp Vault KVv2.
type VaultProvider struct {
	addr       string
	token      string
	mount      string
	pathPrefix string
	http       *http.Client
}

// NewVaultProvider lê config do ambiente. Falha imediatamente se
// VAULT_ADDR ou VAULT_TOKEN não estiverem definidos — Vault sem auth
// não tem caso de uso legítimo no nosso contexto.
func NewVaultProvider() (*VaultProvider, error) {
	addr := os.Getenv("VAULT_ADDR")
	if addr == "" {
		return nil, errors.New("VAULT_ADDR obrigatório")
	}
	token := os.Getenv("VAULT_TOKEN")
	if token == "" {
		return nil, errors.New("VAULT_TOKEN obrigatório")
	}
	mount := os.Getenv("VAULT_KV_MOUNT")
	if mount == "" {
		mount = "secret"
	}
	return &VaultProvider{
		addr:       strings.TrimRight(addr, "/"),
		token:      token,
		mount:      mount,
		pathPrefix: os.Getenv("VAULT_PATH_PREFIX"),
		http:       &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Get lê o segredo em `data.{key}` do path `{mount}/data/{pathPrefix}/{key}`.
// Convenção: cada `secret.Provider.Get(key)` mapeia para um secret Vault
// com **uma** entrada `value` ou onde o subkey é o próprio `key`. Se o
// caller passa "GITLAB_TOKEN", buscamos primeiro o subkey "value" do
// secret "{prefix}/GITLAB_TOKEN"; se vazio, tentamos o subkey "GITLAB_TOKEN"
// do secret "{prefix}/credentials". Essa segunda forma cobre o padrão de
// agrupar credenciais relacionadas no mesmo secret.
func (v *VaultProvider) Get(ctx context.Context, key string) (string, error) {
	if val, err := v.fetchSubkey(ctx, path.Join(v.pathPrefix, key), "value"); err == nil {
		return val, nil
	} else if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	val, err := v.fetchSubkey(ctx, path.Join(v.pathPrefix, "credentials"), key)
	if err != nil {
		return "", err
	}
	return val, nil
}

func (v *VaultProvider) fetchSubkey(ctx context.Context, relPath, subkey string) (string, error) {
	uri := fmt.Sprintf("%s/v1/%s/data/%s", v.addr, v.mount, strings.TrimPrefix(relPath, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return "", fmt.Errorf("build vault request: %w", err)
	}
	req.Header.Set("X-Vault-Token", v.token)
	req.Header.Set("Accept", "application/json")

	resp, err := v.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("vault request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return "", fmt.Errorf("vault http %d: %s", resp.StatusCode, body)
	}

	var envelope struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", fmt.Errorf("decode vault response: %w", err)
	}
	val, ok := envelope.Data.Data[subkey]
	if !ok || val == "" {
		return "", ErrNotFound
	}
	return val, nil
}
