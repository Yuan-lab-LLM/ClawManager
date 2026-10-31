package systembackup

import (
	"context"
	"strings"
	"testing"
)

func TestVerifierIdentityRejectsWriteGrantBeforeRestore(t *testing.T) {
	admin := fixtureDatabase(t)
	_, name := isolatedSchemaDB(t, admin)
	verifier := selectOnlyVerifier(t, admin, name)
	ctx := context.Background()
	if e := RequireReadOnlyVerifier(ctx, verifier, name); e != nil {
		t.Fatal("SELECT-only verifier refused", e)
	}
	var identity string
	if e := verifier.QueryRow("SELECT CURRENT_USER()").Scan(&identity); e != nil {
		t.Fatal(e)
	}
	parts := strings.Split(identity, "@")
	if len(parts) != 2 || parts[1] != "%" || !identifierPattern.MatchString(parts[0]) {
		t.Fatal("unexpected fixture identity")
	}
	if _, e := admin.Exec("GRANT INSERT ON `" + name + "`.* TO '" + parts[0] + "'@'%'"); e != nil {
		t.Fatal(e)
	}
	if e := RequireReadOnlyVerifier(ctx, verifier, name); e == nil {
		t.Fatal("different username with write grant accepted")
	}
}
