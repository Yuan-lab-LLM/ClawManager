package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Proofs are signed by an independently trusted MFA issuer. The application
// only has public verification keys and cannot turn a password login into MFA.
type StrongAuthClaims struct {
	Issuer         string   `json:"iss"`
	Audience       string   `json:"aud"`
	Subject        string   `json:"sub"`
	Session        string   `json:"session"`
	AuthTime       int64    `json:"auth_time"`
	Expires        int64    `json:"exp"`
	Method         string   `json:"method"`
	Nonce          string   `json:"nonce"`
	Endpoint       string   `json:"endpoint"`
	RequestHash    string   `json:"request_hash"`
	IdempotencyKey string   `json:"idempotency_key"`
	ACR            string   `json:"acr,omitempty"`
	AMR            []string `json:"amr,omitempty"`
}

type StrongAuthIssuer struct {
	Issuer     string   `json:"issuer"`
	KeyVersion string   `json:"key_version"`
	PublicKey  []byte   `json:"public_key"`
	Methods    []string `json:"methods"`
	ACR        []string `json:"acr,omitempty"`
	AMR        []string `json:"amr,omitempty"`
}

type StrongAuthVerifier struct {
	Audience      string             `json:"audience"`
	MaxAgeSeconds int64              `json:"max_age_seconds"`
	NonceKey      []byte             `json:"nonce_hmac_key"`
	Issuers       []StrongAuthIssuer `json:"issuers"`
}

func LoadStrongAuthVerifier(path string) (*StrongAuthVerifier, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("invalid strong authentication bootstrap")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return nil, errors.New("invalid strong authentication bootstrap")
	}
	raw := make([]byte, info.Size())
	if _, err = f.ReadAt(raw, 0); err != nil {
		return nil, errors.New("invalid strong authentication bootstrap")
	}
	var v StrongAuthVerifier
	if DecodeStrictJSON(raw, &v) != nil || v.Validate() != nil {
		return nil, errors.New("invalid strong authentication bootstrap")
	}
	return &v, nil
}

func (v *StrongAuthVerifier) Validate() error {
	if v == nil || v.Audience == "" || len(v.Audience) > 128 || v.MaxAgeSeconds < 30 || v.MaxAgeSeconds > 900 || len(v.NonceKey) != 32 || len(v.Issuers) < 1 || len(v.Issuers) > 32 {
		return errors.New("strong_auth_required")
	}
	seen := map[string]bool{}
	for _, i := range v.Issuers {
		id := i.Issuer + "\x00" + i.KeyVersion
		if i.Issuer == "" || len(i.Issuer) > 255 || !refPattern.MatchString(i.KeyVersion) || len(i.PublicKey) != ed25519.PublicKeySize || len(i.Methods) == 0 || seen[id] {
			return errors.New("strong_auth_invalid_issuer")
		}
		seen[id] = true
		for _, m := range i.Methods {
			if !slices.Contains([]string{"webauthn", "totp", "oidc_mfa"}, m) || m == "oidc_mfa" && (len(i.ACR) == 0 || len(i.AMR) == 0) {
				return errors.New("strong_auth_invalid_issuer")
			}
		}
	}
	return nil
}

func (v *StrongAuthVerifier) Verify(proof string, now time.Time) (StrongAuthClaims, error) {
	var c StrongAuthClaims
	if proof == "" || v == nil {
		return c, errors.New("strong_auth_required")
	}
	if err := v.Validate(); err != nil {
		return c, err
	}
	if len(proof) > 8192 {
		return c, errors.New("strong_auth_invalid")
	}
	p := strings.Split(proof, ".")
	if len(p) != 3 {
		return c, errors.New("strong_auth_invalid")
	}
	var h struct {
		Alg  string `json:"alg"`
		Type string `json:"typ"`
		Key  string `json:"kid"`
	}
	hb, e1 := base64.RawURLEncoding.Strict().DecodeString(p[0])
	cb, e2 := base64.RawURLEncoding.Strict().DecodeString(p[1])
	sig, e3 := base64.RawURLEncoding.Strict().DecodeString(p[2])
	if e1 != nil || e2 != nil || e3 != nil || DecodeStrictJSON(hb, &h) != nil || DecodeStrictJSON(cb, &c) != nil || h.Alg != "EdDSA" || h.Type != "sb-mfa+jwt" {
		return c, errors.New("strong_auth_invalid")
	}
	var trusted *StrongAuthIssuer
	for j := range v.Issuers {
		i := &v.Issuers[j]
		if i.Issuer == c.Issuer && i.KeyVersion == h.Key {
			trusted = i
			break
		}
	}
	if trusted == nil {
		return c, errors.New("strong_auth_invalid_issuer")
	}
	if !ed25519.Verify(trusted.PublicKey, []byte(p[0]+"."+p[1]), sig) || c.Audience != v.Audience || c.Subject == "" || c.Session == "" || len(c.Nonce) < 24 || len(c.Nonce) > 256 || !slices.Contains(trusted.Methods, c.Method) {
		return c, errors.New("strong_auth_invalid")
	}
	if c.Method == "oidc_mfa" {
		if !slices.Contains(trusted.ACR, c.ACR) {
			return c, errors.New("strong_auth_invalid")
		}
		for _, m := range trusted.AMR {
			if !slices.Contains(c.AMR, m) {
				return c, errors.New("strong_auth_invalid")
			}
		}
	}
	if err := v.checkTime(c, now); err != nil {
		return c, err
	}
	return c, nil
}

func (v *StrongAuthVerifier) checkTime(c StrongAuthClaims, now time.Time) error {
	if c.AuthTime <= 0 || c.Expires <= c.AuthTime || c.AuthTime > now.Unix()+30 {
		return errors.New("strong_auth_invalid")
	}
	if c.Expires <= now.Unix() || c.AuthTime < now.Unix()-v.MaxAgeSeconds-30 {
		return errors.New("strong_auth_expired")
	}
	return nil
}

func (v *StrongAuthVerifier) nonceDigest(c StrongAuthClaims) string {
	m := hmac.New(sha256.New, v.NonceKey)
	m.Write([]byte(c.Issuer + "\x00" + c.Nonce))
	return hex.EncodeToString(m.Sum(nil))
}

// SessionBinding is deliberately tied to the authenticated access token. A token
// refresh changes the binding; no raw token is sent to the issuer or persisted.
func SessionBinding(authorization string) string {
	if !strings.HasPrefix(authorization, "Bearer ") || len(authorization) > 16384 {
		return ""
	}
	token := strings.TrimPrefix(authorization, "Bearer ")
	if token == "" {
		return ""
	}
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

type ConfigChange struct {
	Expected     uint64       `json:"expected_version"`
	Config       BackupConfig `json:"config"`
	ChangeReason string       `json:"change_reason,omitempty"`
}

func (s Store) SaveConfigAuthorized(ctx context.Context, actor int, key string, input ConfigChange, session, proof string, bindings RoleBindings, v *StrongAuthVerifier) (ConfigVersion, bool, error) {
	if !bindings.Allows(actor, Configure) {
		return ConfigVersion{}, false, errors.New("permission_denied")
	}
	if len(input.ChangeReason) > 512 {
		return ConfigVersion{}, false, errors.New("validation_failed")
	}
	hash, err := RequestHash(input)
	if err != nil {
		return ConfigVersion{}, false, err
	}
	var used *StrongAuthClaims
	return idempotentOperation(ctx, s, actor, "PUT /config", key, input, func(tx *sql.Tx) (ConfigVersion, error) {
		return s.saveConfigTx(ctx, tx, input.Expected, actor, input.Config, func(before, after BackupConfig) error {
			security := SecurityConfigChanged(before, after)
			destructive := after.RetentionSeconds < before.RetentionSeconds || after.MinimumRecoveryPoints < before.MinimumRecoveryPoints
			if security && !bindings.Allows(actor, SecurityConfigure) || destructive && !bindings.Allows(actor, Destructive) {
				return errors.New("permission_denied")
			}
			if destructive && strings.TrimSpace(input.ChangeReason) == "" {
				return errors.New("validation_failed")
			}
			if !security && !destructive {
				return nil
			}
			var now time.Time
			if e := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); e != nil {
				return e
			}
			c, e := v.Verify(proof, now)
			if e != nil {
				return e
			}
			if c.Subject != strconv.Itoa(actor) || session == "" || c.Session != session || c.Audience != s.InstallationID {
				return errors.New("strong_auth_invalid")
			}
			digest := v.nonceDigest(c)
			var n int
			if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_backup_strong_auth_nonces WHERE nonce_digest=?", digest).Scan(&n); e != nil {
				return e
			}
			if n != 0 {
				return errors.New("strong_auth_replayed")
			}
			if c.Endpoint != "PUT /config" || c.RequestHash != hash || c.IdempotencyKey != key {
				return errors.New("strong_auth_invalid")
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO system_backup_strong_auth_nonces(nonce_digest,actor_id,endpoint,idempotency_key,request_hash,expires_at) VALUES(?,?,?,?,?,?)", digest, actor, c.Endpoint, key, hash, time.Unix(c.Expires, 0).UTC()); e != nil {
				return e
			}
			if e = appendEvent(ctx, tx, "", "strong_auth_consumed", map[string]any{"actor_id": actor, "method": c.Method, "change_reason": input.ChangeReason}); e != nil {
				return e
			}
			used = &c
			return nil
		})
	}, func(tx *sql.Tx) error {
		if used == nil {
			return nil
		}
		var now time.Time
		if e := tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); e != nil {
			return e
		}
		return v.checkTime(*used, now)
	})
}
