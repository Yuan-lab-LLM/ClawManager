package systembackup

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// All new signature inputs bind a protected header and canonical unsigned
// payload. Legacy functional artifacts remain verifiable without this header.
type SignatureHeader struct {
	Algorithm        string `json:"alg"`
	KeyVersion       string `json:"kid"`
	Purpose          string `json:"purpose"`
	Canonicalization string `json:"canonicalization"`
}

func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jsoncanonicalizer.Transform(raw)
}
func jsoncanonicalizerTransform(raw []byte) ([]byte, error) { return jsoncanonicalizer.Transform(raw) }

func signatureInput(domain string, payload any, h SignatureHeader) ([]byte, error) {
	if h.Algorithm != "Ed25519" || !refPattern.MatchString(h.KeyVersion) || h.Canonicalization != "RFC8785" || (h.Purpose != "artifact" && h.Purpose != "acceptance" && h.Purpose != "catalog") {
		return nil, errors.New("signature_invalid")
	}
	raw, err := canonicalJSON(struct {
		Header  SignatureHeader `json:"protected"`
		Payload any             `json:"payload"`
	}{h, payload})
	if err != nil {
		return nil, err
	}
	return append([]byte(domain+"\n"), raw...), nil
}

func signPurpose(domain, purpose string, payload any, key ed25519.PrivateKey, version string) (ArtifactSignature, error) {
	if len(key) != ed25519.PrivateKeySize {
		return ArtifactSignature{}, errors.New("signing_key_unavailable")
	}
	h := SignatureHeader{"Ed25519", version, purpose, "RFC8785"}
	raw, err := signatureInput(domain, payload, h)
	if err != nil {
		return ArtifactSignature{}, err
	}
	return ArtifactSignature{Algorithm: "Ed25519", KeyVersion: version, Value: ed25519.Sign(key, raw), Protected: &h}, nil
}

func verifyPurpose(domain, purpose string, payload any, s ArtifactSignature, key ed25519.PublicKey, version string) error {
	if s.Protected == nil || s.Protected.Purpose != purpose || s.Protected.KeyVersion != version || s.Algorithm != "Ed25519" || s.KeyVersion != version || len(key) != ed25519.PublicKeySize {
		return errors.New("signature_invalid")
	}
	raw, err := signatureInput(domain, payload, *s.Protected)
	if err != nil {
		return err
	}
	if !ed25519.Verify(key, raw, s.Value) {
		return errors.New("signature_invalid")
	}
	return nil
}
