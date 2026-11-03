package services

import (
	"context"
	"testing"

	"clawreef/internal/models"
)

func TestManagedPreviewInterruptRetainsAdmissionUntilJoinedClose(t *testing.T) {
	p, _, id, _ := previewFixture(t)
	g, err := p.Sign(context.Background(), id, "site/index.html", "static")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ManagedTeamPreviewFile
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	for i := 0; i < 64; i++ {
		f, err := p.Open(context.Background(), previewPath(t, g))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	if err := files[0].Interrupt(); err != nil {
		t.Fatal(err)
	}
	if extra, err := p.Open(context.Background(), previewPath(t, g)); err == nil {
		extra.Close()
		t.Fatal("interrupted stream released admission before checker joined")
	}
	_ = files[0].Close()
	extra, err := p.Open(context.Background(), previewPath(t, g))
	if err != nil {
		t.Fatal("joined stream did not release admission", err)
	}
	_ = extra.Close()
}

func TestManagedPreviewLegacyBindingRevokedByFirstProtection(t *testing.T) {
	p, teams, _, _ := previewFixture(t)
	f, err := p.OpenLegacy(context.Background(), 12, 3, "site", "index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	teams.team.PreviewSecurityProfile = models.TeamPreviewSecurityProfile
	if err := f.Check(context.Background()); err == nil {
		t.Fatal("active legacy stream retained Team authority")
	}
	if extra, err := p.OpenLegacy(context.Background(), 12, 3, "site", "index.html"); err == nil {
		extra.Close()
		t.Fatal("new legacy request used retired authority")
	}
}
