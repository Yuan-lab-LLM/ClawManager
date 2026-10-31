package repository

import (
	"clawreef/internal/models"
	"context"
	"github.com/upper/db/v4"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type previewUpdateSession struct {
	db.Session
	written *models.Team
}

func (s *previewUpdateSession) Collection(string) db.Collection { return previewUpdateCollection{s: s} }

type previewUpdateCollection struct {
	db.Collection
	s *previewUpdateSession
}

func (c previewUpdateCollection) Find(...interface{}) db.Result { return previewUpdateResult{s: c.s} }

type previewUpdateResult struct {
	db.Result
	s *previewUpdateSession
}

func (r previewUpdateResult) Update(value interface{}) error {
	copy := *(value.(*models.Team))
	r.s.written = &copy
	return nil
}
func TestTeamPreviewGenericUpdateCannotChangeMarker(t *testing.T) {
	field, ok := reflect.TypeOf(models.Team{}).FieldByName("PreviewSecurityProfile")
	if !ok || field.Tag.Get("db") != "preview_security_profile,omitempty" {
		t.Fatal("marker must omit zero from generic updates")
	}
	for _, marker := range []string{"", models.TeamPreviewSecurityProfile, "unknown-future"} {
		sess := &previewUpdateSession{}
		repo := &teamRepository{sess: sess}
		team := &models.Team{ID: 12, Name: "edited", PreviewSecurityProfile: marker}
		if e := repo.UpdateTeam(team); e != nil {
			t.Fatal(e)
		}
		if sess.written.PreviewSecurityProfile != "" || team.PreviewSecurityProfile != marker || sess.written.Name != "edited" {
			t.Fatal("generic update writes security authority or mutates caller")
		}
	}
}

// Reuses the explicitly opted-in disposable localhost database fixture. This
// verifies actual persistence/SQL collation semantics when MySQL is available.
func TestTeamPreviewSecurityPersistence(t *testing.T) {
	instances := runtimeSecurityMySQL(t)
	repo := &teamRepository{sess: instances.sess}
	var columns []string
	typ := reflect.TypeOf(models.Team{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.Split(field.Tag.Get("db"), ",")[0]
		if name == "preview_security_profile" {
			continue
		}
		kind := field.Type
		if kind.Kind() == reflect.Pointer {
			kind = kind.Elem()
		}
		sqlType := "TEXT NULL"
		if kind.Kind() == reflect.Int {
			sqlType = "BIGINT NULL"
		}
		if kind == reflect.TypeOf(time.Time{}) {
			sqlType = "DATETIME(6) NULL"
		}
		if name == "id" {
			sqlType = "BIGINT PRIMARY KEY"
		}
		columns = append(columns, "`"+name+"` "+sqlType)
	}
	if _, err := repo.sess.SQL().Exec("CREATE TABLE teams (" + strings.Join(columns, ",") + ")"); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../db/migrations/078_add_team_preview_security_profile.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.sess.SQL().Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	team := &models.Team{ID: 12, UserID: 3, Status: models.TeamStatusRunning, Name: "before", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if _, err = repo.sess.Collection("teams").Insert(team); err != nil {
		t.Fatal(err)
	}
	stale := *team
	ctx := context.Background()
	if err = repo.ProtectTeamPreview(ctx, 12, 4); err == nil {
		t.Fatal("wrong owner marked Team")
	}
	if err = repo.ProtectTeamPreview(ctx, 12, 3); err != nil {
		t.Fatal(err)
	}
	stale.Name = "edited"
	if err = repo.UpdateTeam(&stale); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetTeamByIDContext(ctx, 12)
	if err != nil || stored.PreviewSecurityProfile != models.TeamPreviewSecurityProfile || stored.Name != "edited" {
		t.Fatalf("stale update downgraded marker: %+v %v", stored, err)
	}
	if err = repo.ProtectTeamPreview(ctx, 12, 3); err != nil {
		t.Fatal("idempotent protection failed", err)
	}
	for _, unknown := range []string{"MANAGED-PREVIEW-V3", "managed-preview-v3 ", "future"} {
		if _, err = repo.sess.SQL().Exec("UPDATE teams SET preview_security_profile = ? WHERE id = 12", unknown); err != nil {
			t.Fatal(err)
		}
		if err = repo.ProtectTeamPreview(ctx, 12, 3); err == nil {
			t.Fatal("unknown marker normalized")
		}
		stale.PreviewSecurityProfile = models.TeamPreviewSecurityProfile
		if err = repo.UpdateTeam(&stale); err != nil {
			t.Fatal(err)
		}
		stored, err = repo.GetTeamByIDContext(ctx, 12)
		if err != nil || stored.PreviewSecurityProfile != unknown {
			t.Fatal("unknown marker overwritten")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err = repo.ProtectTeamPreview(canceled, 12, 3); err == nil {
		t.Fatal("canceled SQL accepted")
	}
}

type previewMemberContextSession struct {
	db.Session
	ctx       context.Context
	table     string
	condition []interface{}
	limit     int
}

func (s *previewMemberContextSession) WithContext(ctx context.Context) db.Session {
	s.ctx = ctx
	return s
}
func (s *previewMemberContextSession) Collection(name string) db.Collection {
	s.table = name
	return previewMemberContextCollection{s: s}
}

type previewMemberContextCollection struct {
	db.Collection
	s *previewMemberContextSession
}

func (c previewMemberContextCollection) Find(args ...interface{}) db.Result {
	c.s.condition = args
	return previewMemberContextResult{s: c.s}
}

type previewMemberContextResult struct {
	db.Result
	s *previewMemberContextSession
}

func (r previewMemberContextResult) Limit(n int) db.Result { r.s.limit = n; return r }
func (r previewMemberContextResult) All(any) error         { return r.s.ctx.Err() }
func TestTeamPreviewMemberEnumerationIsContextualAndBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := &previewMemberContextSession{}
	repo := &teamRepository{sess: session}
	if _, err := repo.ListMembersByTeamIDContext(ctx, 12); err != context.Canceled {
		t.Fatal("enumeration detached SQL context", err)
	}
	if session.ctx != ctx || session.table != "team_members" || session.limit != 257 || !reflect.DeepEqual(session.condition, []interface{}{db.Cond{"team_id": 12}}) {
		t.Fatal("unbounded or unscoped member enumeration")
	}
}
