// Criptografia simétrica para tokens OAuth guardados em DB.
//
// Algoritmo: AES-256-GCM (authenticated encryption). Nonce de 12 bytes
// gerado por chamada (não-reutilizado), prepended ao ciphertext.
//
// A master key vem do env OAUTH_ENCRYPTION_KEY como base64 de 32 bytes.
// Gerar com: `openssl rand -base64 32`.
//
// Formato no banco (TEXT): base64(nonce || ciphertext || gcmTag).
// O GCM já inclui o tag de auth no ciphertext; só precisamos extrair
// o nonce do início.
package atlassian

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

// CipherKey carregada do env. Em testes, sobrescrita via SetKey.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipherFromEnv lê OAUTH_ENCRYPTION_KEY (base64 32 bytes).
func NewCipherFromEnv() (*Cipher, error) {
	keyB64 := os.Getenv("OAUTH_ENCRYPTION_KEY")
	if keyB64 == "" {
		return nil, errors.New("OAUTH_ENCRYPTION_KEY obrigatório (base64 de 32 bytes — `openssl rand -base64 32`)")
	}
	return NewCipherFromBase64(keyB64)
}

// NewCipherFromBase64 constrói o Cipher a partir de string base64.
// Útil para testes (não passa pelo env).
func NewCipherFromBase64(keyB64 string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("OAUTH_ENCRYPTION_KEY base64 inválido: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("OAUTH_ENCRYPTION_KEY precisa ter 32 bytes; tem %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}
	return &Cipher{aead: gcm}, nil
}

// Encrypt cifra `plaintext` e devolve base64(nonce||ciphertext||tag).
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	ct := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	envelope := append(nonce, ct...)
	return base64.StdEncoding.EncodeToString(envelope), nil
}

// Decrypt reverte. Devolve string vazia se input vazio.
func (c *Cipher) Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	envelope, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(envelope) < ns+c.aead.Overhead() {
		return "", errors.New("ciphertext muito curto")
	}
	nonce := envelope[:ns]
	ct := envelope[ns:]
	pt, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(pt), nil
}
