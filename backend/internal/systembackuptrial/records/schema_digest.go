package records

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func SchemaDigest(t Table) string {
	t.Rows = nil
	b, _ := json.Marshal(t)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
