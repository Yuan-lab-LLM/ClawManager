// Package nbkeys is shared by the admin, Gateway and Core without coupling
// API keys to refresh-token sessions. Only PublicKey may leave the server.
package nbkeys

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const MaxKeys = 20
const MaxDocumentBytes = 64 << 10

type Key struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	SecretHash string     `json:"secret_hash"`
	Scopes     []string   `json:"scopes"`
	Enabled    bool       `json:"enabled"`
	ExpiresAt  time.Time  `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
	CreatedBy  int        `json:"created_by"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type PublicKey struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Scopes          []string   `json:"scopes"`
	EffectiveScopes []string   `json:"effective_scopes"`
	Enabled         bool       `json:"enabled"`
	ExpiresAt       time.Time  `json:"expires_at"`
	CreatedAt       time.Time  `json:"created_at"`
	CreatedBy       int        `json:"created_by"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

func New(userID int, name string, scopes []string, expires time.Time, actor int) (Key, string, error) {
	var k Key
	id := make([]byte, 12)
	secret := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return k, "", err
	}
	if _, err := rand.Read(secret); err != nil {
		return k, "", err
	}
	k = Key{ID: hex.EncodeToString(id), Name: name, Scopes: scopes, Enabled: true, ExpiresAt: expires.UTC(), CreatedAt: time.Now().UTC(), CreatedBy: actor}
	raw := fmt.Sprintf("nbk1.%d.%s.%s", userID, k.ID, base64.RawURLEncoding.EncodeToString(secret))
	k.SecretHash = digest(raw)
	return k, raw, nil
}

func Parse(raw string) (int, string, error) {
	bad := fmt.Errorf("invalid API key")
	if len(raw) > 256 {
		return 0, "", bad
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 4 || parts[0] != "nbk1" {
		return 0, "", bad
	}
	uid, err := strconv.Atoi(parts[1])
	if err != nil || uid <= 0 || strconv.Itoa(uid) != parts[1] {
		return 0, "", bad
	}
	id, err := hex.DecodeString(parts[2])
	if err != nil || len(id) != 12 {
		return 0, "", bad
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(secret) != 32 {
		return 0, "", bad
	}
	return uid, parts[2], nil
}

func digest(raw string) string { sum := sha256.Sum256([]byte(raw)); return hex.EncodeToString(sum[:]) }
func (k Key) Verify(raw string) bool {
	return subtle.ConstantTimeCompare([]byte(k.SecretHash), []byte(digest(raw))) == 1
}
func (k Key) Active(now time.Time) bool {
	return k.Enabled && k.RevokedAt == nil && now.Before(k.ExpiresAt)
}
func (k Key) EffectiveScopes(ceiling []string, now time.Time) []string {
	result := []string{}
	if !k.Active(now) {
		return result
	}
	for _, s := range k.Scopes {
		for _, allowed := range ceiling {
			if s == allowed {
				result = append(result, s)
				break
			}
		}
	}
	return result
}
func (k Key) Public(ceiling []string, now time.Time) PublicKey {
	return PublicKey{k.ID, k.Name, k.Scopes, k.EffectiveScopes(ceiling, now), k.Enabled, k.ExpiresAt, k.CreatedAt, k.CreatedBy, k.RevokedAt}
}
func Decode(raw *string) ([]Key, error) {
	keys := []Key{}
	if raw == nil || *raw == "" {
		return keys, nil
	}
	if len(*raw) > MaxDocumentBytes {
		return nil, fmt.Errorf("API key document too large")
	}
	if err := json.Unmarshal([]byte(*raw), &keys); err != nil {
		return nil, fmt.Errorf("invalid API key configuration")
	}
	if len(keys) > MaxKeys {
		return nil, fmt.Errorf("too many API keys")
	}
	return keys, nil
}
