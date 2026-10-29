package runtimeauth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
)

const AudienceModel = "model"
const AudienceEgress = "egress"
const AudienceServices = "services"

const (
	ttlSeconds      = int64(15 * 60)
	maxTokenBytes   = 4096
	signatureDomain = "clawmanager/runtime-capability/v1\x00"
)

var errCapability = errors.New("invalid runtime capability")

// Identity describes an active runtime assignment. Lookup must return an error
// when the assignment is inactive or absent.
type Identity struct {
	InstanceID, UserID, Generation int
	PodUID                         string
}

// Lookup reads the current authoritative identity for an instance ID.
type Lookup func(context.Context, int) (Identity, error)

// Service issues short-lived, audience-bound capabilities. It is safe for
// concurrent use when its Lookup is safe for concurrent use.
type Service struct {
	key    []byte
	lookup Lookup
	now    func() time.Time
}

type claims struct {
	Version    int    `json:"v"`
	InstanceID int    `json:"instance_id"`
	UserID     int    `json:"user_id"`
	Generation int    `json:"generation"`
	PodUID     string `json:"pod_uid"`
	Audience   string `json:"aud"`
	Issued     int64  `json:"iat"`
	Expires    int64  `json:"exp"`
}

// New requires a dedicated signing key of at least 32 bytes.
func New(key []byte, lookup Lookup) (*Service, error) {
	if len(key) < 32 || lookup == nil {
		return nil, errors.New("invalid runtime capability configuration")
	}
	return &Service{key: append([]byte(nil), key...), lookup: lookup, now: time.Now}, nil
}

// Issue checks the current assignment before issuing a fifteen-minute token.
func (s *Service) Issue(ctx context.Context, identity Identity, audience string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validIdentity(identity) || !validAudience(audience) {
		return "", errCapability
	}
	if err := s.authorize(ctx, identity); err != nil {
		return "", err
	}
	issued := s.now().Unix()
	if issued <= 0 || issued > math.MaxInt64-ttlSeconds {
		return "", errCapability
	}
	c := claims{
		Version: 1, InstanceID: identity.InstanceID, UserID: identity.UserID,
		Generation: identity.Generation, PodUID: identity.PodUID,
		Audience: audience, Issued: issued, Expires: issued + ttlSeconds,
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", errCapability
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	token := "v1." + encoded + "." + base64.RawURLEncoding.EncodeToString(s.sign(encoded))
	if len(token) > maxTokenBytes {
		return "", errCapability
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return token, nil
}

// Verify validates the token and rechecks the authoritative runtime assignment.
func (s *Service) Verify(ctx context.Context, token, audience string) (Identity, error) {
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	if len(token) > maxTokenBytes || !validAudience(audience) {
		return Identity{}, errCapability
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return Identity{}, errCapability
	}
	signature, err := decode(parts[2])
	if err != nil || !hmac.Equal(signature, s.sign(parts[1])) {
		return Identity{}, errCapability
	}
	payload, err := decode(parts[1])
	if err != nil {
		return Identity{}, errCapability
	}
	var c claims
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Identity{}, errCapability
	}
	// A single canonical JSON spelling rejects duplicate, missing, case-aliased,
	// escaped or trailing fields and whitespace, including decoder ambiguities.
	canonical, err := json.Marshal(c)
	if err != nil || !bytes.Equal(payload, canonical) {
		return Identity{}, errCapability
	}
	identity := Identity{InstanceID: c.InstanceID, UserID: c.UserID, Generation: c.Generation, PodUID: c.PodUID}
	if c.Version != 1 || c.Audience != audience || !validIdentity(identity) || !s.validTime(c) {
		return Identity{}, errCapability
	}
	if err := s.authorize(ctx, identity); err != nil {
		return Identity{}, err
	}
	// Lookup may block until after the capability expires.
	if !s.validTime(c) {
		return Identity{}, errCapability
	}
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (s *Service) authorize(ctx context.Context, identity Identity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := s.lookup(ctx, identity.InstanceID)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil || current != identity {
		return errCapability
	}
	return nil
}

func (s *Service) sign(payload string) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(signatureDomain))
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func (s *Service) validTime(c claims) bool {
	now := s.now().Unix()
	return c.Issued > 0 && c.Issued <= math.MaxInt64-ttlSeconds &&
		c.Expires == c.Issued+ttlSeconds && c.Issued <= now && now < c.Expires
}

func decode(encoded string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return nil, errCapability
	}
	return decoded, nil
}

func validAudience(audience string) bool {
	return audience == AudienceModel || audience == AudienceEgress || audience == AudienceServices
}

func validIdentity(identity Identity) bool {
	if identity.InstanceID <= 0 || identity.InstanceID > math.MaxInt32 ||
		identity.UserID <= 0 || identity.UserID > math.MaxInt32 ||
		identity.Generation <= 0 || identity.Generation > math.MaxInt32 ||
		len(identity.PodUID) == 0 || len(identity.PodUID) > 128 {
		return false
	}
	for _, c := range identity.PodUID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
