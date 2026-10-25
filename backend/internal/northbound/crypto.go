package northbound

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"strings"
)

var rawURL = base64.RawURLEncoding

type JWEDecryptor struct {
	privateKey *rsa.PrivateKey
	keyID      string
}

type PublicJWK struct {
	KeyType string `json:"kty"`
	KeyID   string `json:"kid"`
	N       string `json:"n"`
	E       string `json:"e"`
}

type jweHeader struct {
	KeyID      string `json:"kid"`
	Algorithm  string `json:"alg"`
	Encryption string `json:"enc"`
}

func LoadJWEDecryptor(path, keyID string) (*JWEDecryptor, error) {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("read northbound JWE private key: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("northbound JWE private key is not PEM encoded")
	}
	var key *rsa.PrivateKey
	if parsed, parseErr := x509.ParsePKCS1PrivateKey(block.Bytes); parseErr == nil {
		key = parsed
	} else if parsedAny, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes); parseErr == nil {
		var ok bool
		key, ok = parsedAny.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("northbound JWE private key must be RSA")
		}
	} else {
		return nil, fmt.Errorf("parse northbound JWE private key")
	}
	if key.N.BitLen() < 3072 {
		return nil, fmt.Errorf("northbound JWE RSA key must be at least 3072 bits")
	}
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("invalid northbound JWE RSA key: %w", err)
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return nil, fmt.Errorf("northbound JWE key id is required")
	}
	return &JWEDecryptor{privateKey: key, keyID: keyID}, nil
}

func NewJWEDecryptor(key *rsa.PrivateKey, keyID string) (*JWEDecryptor, error) {
	if key == nil || key.N == nil || key.N.BitLen() < 2048 {
		return nil, fmt.Errorf("valid RSA private key is required")
	}
	if strings.TrimSpace(keyID) == "" {
		return nil, fmt.Errorf("key id is required")
	}
	return &JWEDecryptor{privateKey: key, keyID: strings.TrimSpace(keyID)}, nil
}

func (d *JWEDecryptor) KeyID() string { return d.keyID }

func (d *JWEDecryptor) PublicJWK() PublicJWK {
	exponent := big.NewInt(int64(d.privateKey.PublicKey.E)).Bytes()
	return PublicJWK{
		KeyType: "RSA",
		KeyID:   d.keyID,
		N:       rawURL.EncodeToString(d.privateKey.PublicKey.N.Bytes()),
		E:       rawURL.EncodeToString(exponent),
	}
}

func (d *JWEDecryptor) DecryptCompact(compact string) ([]byte, error) {
	parts := strings.Split(strings.TrimSpace(compact), ".")
	if len(parts) != 5 {
		return nil, fmt.Errorf("invalid JWE compact serialization")
	}
	protectedJSON, err := rawURL.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode JWE protected header: %w", err)
	}
	var header jweHeader
	if err := json.Unmarshal(protectedJSON, &header); err != nil {
		return nil, fmt.Errorf("decode JWE protected header JSON: %w", err)
	}
	var protectedFields map[string]json.RawMessage
	if err := json.Unmarshal(protectedJSON, &protectedFields); err != nil || len(protectedFields) != 3 {
		return nil, fmt.Errorf("unsupported JWE protected header")
	}
	for _, required := range []string{"kid", "alg", "enc"} {
		if _, ok := protectedFields[required]; !ok {
			return nil, fmt.Errorf("unsupported JWE protected header")
		}
	}
	if header.KeyID != d.keyID || header.Algorithm != "RSA-OAEP-256" || header.Encryption != "A256GCM" {
		return nil, fmt.Errorf("unsupported JWE header")
	}
	encryptedKey, err := rawURL.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode JWE encrypted key: %w", err)
	}
	cek, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, d.privateKey, encryptedKey, nil)
	if err != nil || len(cek) != 32 {
		return nil, fmt.Errorf("decrypt JWE content encryption key")
	}
	defer clear(cek)
	iv, err := rawURL.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode JWE IV: %w", err)
	}
	ciphertext, err := rawURL.DecodeString(parts[3])
	if err != nil {
		return nil, fmt.Errorf("decode JWE ciphertext: %w", err)
	}
	tag, err := rawURL.DecodeString(parts[4])
	if err != nil {
		return nil, fmt.Errorf("decode JWE tag: %w", err)
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("initialize JWE cipher: %w", err)
	}
	var gcm cipher.AEAD
	gcm, err = cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize JWE GCM: %w", err)
	}
	if len(iv) != gcm.NonceSize() || len(tag) != gcm.Overhead() {
		return nil, fmt.Errorf("invalid JWE IV or tag length")
	}
	sealed := make([]byte, 0, len(ciphertext)+len(tag))
	sealed = append(sealed, ciphertext...)
	sealed = append(sealed, tag...)
	plaintext, err := gcm.Open(nil, iv, sealed, []byte(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("authenticate JWE ciphertext")
	}
	return plaintext, nil
}

func randomToken(prefix string, bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + rawURL.EncodeToString(value), nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func hmacHex(secret, value string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}
