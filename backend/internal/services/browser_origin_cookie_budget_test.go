package services

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBrowserOriginIssuedCookieHeaderBudget(t *testing.T) {
	svc := NewInstanceAccessService()
	defer svc.Stop()
	var values []string
	maxToken := 0
	for id := 1; id <= 128; id++ {
		tok, err := svc.GenerateBoundToken(1, id, "deepseek-harness", fmt.Sprintf("https://10.130.81.54:%d/", 31400+id-1), "deepseek-harness-runtime-workbench.clawmanager-system.svc.cluster.local:6100", 6004, time.Hour, "iei:"+strings.Repeat("a", 64))
		if err != nil {
			t.Fatal(err)
		}
		maxToken = max(maxToken, len(tok.Token))
		values = append(values, fmt.Sprintf("instance_access_%d=%s", id, tok.Token))
	}
	header := strings.Join(values, "; ")
	t.Logf("128 real issued scoped JWT cookies=%d bytes; maximum token=%d bytes", len(header), maxToken)
	if len(header)+4096 > 128<<10 {
		t.Fatal("cookie header exceeds shared-gateway128KiB budget")
	}
}
