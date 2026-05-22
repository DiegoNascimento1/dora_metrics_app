package atlassian

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func makeKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestCipher_RoundTrip(t *testing.T) {
	c, err := NewCipherFromBase64(makeKey(t))
	if err != nil {
		t.Fatal(err)
	}
	plain := "ATATT3xFfGF0-secret-token-12345"
	enc, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == "" || strings.Contains(enc, plain) {
		t.Error("ciphertext vazio ou contém plaintext")
	}
	got, err := c.Decrypt(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plain {
		t.Errorf("decrypt = %q, want %q", got, plain)
	}
}

func TestCipher_EmptyStringPassesThrough(t *testing.T) {
	c, _ := NewCipherFromBase64(makeKey(t))
	enc, err := c.Encrypt("")
	if err != nil || enc != "" {
		t.Errorf("empty input deveria voltar empty: %q / %v", enc, err)
	}
	dec, err := c.Decrypt("")
	if err != nil || dec != "" {
		t.Errorf("empty enc deveria voltar empty: %q / %v", dec, err)
	}
}

func TestCipher_DifferentNonceProducesDifferentCiphertext(t *testing.T) {
	c, _ := NewCipherFromBase64(makeKey(t))
	plain := "same input"
	a, _ := c.Encrypt(plain)
	b, _ := c.Encrypt(plain)
	if a == b {
		t.Error("nonce reuse — ciphertexts iguais para mesmo input")
	}
}

func TestCipher_TamperedCiphertextRejected(t *testing.T) {
	c, _ := NewCipherFromBase64(makeKey(t))
	enc, _ := c.Encrypt("secret")
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[len(raw)-1] ^= 0xFF
	tampered := base64.StdEncoding.EncodeToString(raw)
	if _, err := c.Decrypt(tampered); err == nil {
		t.Error("GCM aceitou ciphertext adulterado — auth tag não checado")
	}
}

func TestCipher_WrongKeyFailsToDecrypt(t *testing.T) {
	c1, _ := NewCipherFromBase64(makeKey(t))
	c2, _ := NewCipherFromBase64(makeKey(t))
	enc, _ := c1.Encrypt("secret")
	if _, err := c2.Decrypt(enc); err == nil {
		t.Error("decifrou com chave diferente — auth falhou")
	}
}

func TestNewCipherFromBase64_RejectsWrongLength(t *testing.T) {
	for _, b64 := range []string{
		base64.StdEncoding.EncodeToString(make([]byte, 16)), // AES-128 não aceito
		base64.StdEncoding.EncodeToString(make([]byte, 64)), // grande demais
		"not-base64-!!!",
	} {
		if _, err := NewCipherFromBase64(b64); err == nil {
			t.Errorf("aceitou chave inválida %q", b64)
		}
	}
}

func TestNewCipherFromEnv_RequiresVar(t *testing.T) {
	t.Setenv("OAUTH_ENCRYPTION_KEY", "")
	if _, err := NewCipherFromEnv(); err == nil {
		t.Error("env vazio deveria errar")
	}
}
