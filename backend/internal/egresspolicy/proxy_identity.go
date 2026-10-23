package egresspolicy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// InstanceProxyPassword derives a proxy-only credential from the instance token.
// Including the instance ID binds the credential to one gateway in a shared Pod.
func InstanceProxyPassword(instanceID int, accessToken string) string {
	mac := hmac.New(sha256.New, []byte(accessToken))
	_, _ = fmt.Fprintf(mac, "clawmanager-egress-v1:%d", instanceID)
	return hex.EncodeToString(mac.Sum(nil))
}
