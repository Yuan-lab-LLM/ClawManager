package northbound

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/teamtemplate"
	"github.com/gin-gonic/gin"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

type ownerTeamTestInstances struct {
	services.InstanceService
	repo repository.InstanceRepository
}

func (s ownerTeamTestInstances) GetByID(id int) (*models.Instance, error) { return s.repo.GetByID(id) }

type ownerTeamTestEngine struct {
	services.TeamService
	repo     repository.TeamRepository
	dispatch func(int, int, services.DispatchTeamTaskRequest) (*services.TeamTaskPayload, error)
}

func (s ownerTeamTestEngine) DispatchTask(uid, id int, req services.DispatchTeamTaskRequest) (*services.TeamTaskPayload, error) {
	return s.dispatch(uid, id, req)
}

func (s ownerTeamTestEngine) GetTeam(userID, id int) (*services.TeamDetailsPayload, error) {
	team, err := s.repo.GetTeamByID(id)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembersByTeamID(id)
	return &services.TeamDetailsPayload{Team: team, Members: members, Tasks: []services.TeamTaskPayload{}, Events: []services.TeamEventPayload{}}, err
}

func ownerTeamMySQL(t *testing.T) db.Session {
	t.Helper()
	portText := os.Getenv("TEST_OWNER_TEAM_MYSQL_PORT")
	if portText == "" {
		t.Skip("disposable localhost MySQL required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("invalid local fixture port")
	}
	root, err := mysql.Open(mysql.ConnectionURL{Host: "127.0.0.1:" + portText, User: "root", Database: "mysql"})
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("owner_team_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.SQL().Exec("DROP DATABASE " + name); _ = root.Close() })
	session, err := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Database: name, MaxOpenConns: 12, MaxIdleConns: 6})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestOwnerTeamMySQLIsolationIdempotencyAndLifecycle(t *testing.T) {
	session := ownerTeamMySQL(t)
	gin.SetMode(gin.TestMode)
	repo := repository.NewTeamRepository(session)
	store := repository.NewOwnerTeamRepository(session)
	instances := repository.NewInstanceRepository(session)
	users := repository.NewUserRepository(session)
	user := &models.User{Username: "owner_provider_2", Email: "provider2@example.com", PasswordHash: "test-only", Role: "user", AuthProvider: "local", IsActive: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := users.Create(user); err != nil {
		t.Fatal(err)
	}
	legacy := &models.Team{UserID: 1, Name: "legacy-team", Status: "running", SharedMountPath: "/team", RedisEventsLastID: "0-0", CommunicationMode: "leader_mediated"}
	if err := repo.CreateTeam(legacy); err != nil {
		t.Fatal(err)
	}
	create := func(uid int, owner, name string) *models.Team {
		t.Helper()
		label := "同名团队"
		team := &models.Team{UserID: uid, Owner: &owner, DisplayName: &label, ResourceScope: models.TeamScopeNorthbound, Name: name, Status: "running", SharedMountPath: "/team", RedisEventsLastID: "0-0", CommunicationMode: "leader_mediated"}
		if err := repo.CreateTeam(team); err != nil {
			t.Fatal(err)
		}
		return team
	}
	aTeam := create(1, "a@example.com", "nb-a")
	_ = create(1, "b@example.com", "nb-b")
	_ = create(user.ID, "a@example.com", "nb-other-provider")
	if items, err := repo.ListTeamsByUserID(1, 0, 100); err != nil || len(items) != 1 || items[0].ID != legacy.ID {
		t.Fatalf("legacy list leaked owner teams: %#v %v", items, err)
	}
	if count, err := repo.CountTeamsByUserID(1); err != nil || count != 1 {
		t.Fatalf("legacy count mismatch: %d %v", count, err)
	}
	if items, n, err := store.List(1, " A@example.com ", "", 0, 20); err != nil || n != 1 || len(items) != 1 {
		t.Fatalf("owner normalization/provider boundary: %d %v", n, err)
	}
	if _, n, err := store.List(0, "a@example.com", "", 0, 20); err != nil || n != 2 {
		t.Fatalf("IEI verified-owner list mismatch: %d %v", n, err)
	}
	if got, err := store.Get(1, "b@example.com", aTeam.ID); err != nil || got != nil {
		t.Fatal("cross-owner read accepted")
	}
	if got, err := store.Get(user.ID, "a@example.com", aTeam.ID); err != nil || got != nil {
		t.Fatal("cross-provider read accepted")
	}
	if got, err := store.Get(1, "a@example.com", legacy.ID); err != nil || got != nil {
		t.Fatal("legacy Team was inferred as owned")
	}
	// Exercise the actual Core middleware, signed internal identity, persisted
	// session scope and revocation rather than injecting a ready principal.
	nbRepo := repository.NewNorthboundRepository(session)
	now := time.Now().UTC()
	coreSession := &models.NorthboundSession{SessionID: "owner-core-fixture", UserID: 1, RefreshTokenHash: strings.Repeat("c", 64), RefreshTokenHistory: "[]", ScopesJSON: `["teams:read"]`, Status: "active", AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := nbRepo.CreateSession(coreSession); err != nil {
		t.Fatal(err)
	}
	coreAPI := NewOwnerTeamAPI(session, ownerTeamTestEngine{repo: repo}, ownerTeamTestInstances{repo: instances}, users, nil, true, 1)
	coreService := NewCoreService(nbRepo, users, ownerTeamTestInstances{repo: instances}, nil, config.NorthboundConfig{})
	coreService.OwnerTeams = coreAPI
	secret := strings.Repeat("core-test-", 5)
	coreRouter := gin.New()
	RegisterCoreRoutes(coreRouter, NewCoreHandler(coreService, secret))
	coreRequest := func(token string) int {
		req := httptest.NewRequest("GET", "/internal/northbound/v1/teams?owner=a%40example.com", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		coreRouter.ServeHTTP(out, req)
		return out.Code
	}
	if code := coreRequest(""); code != 401 {
		t.Fatalf("Core accepted missing identity: %d", code)
	}
	token, err := issueInternalToken(secret, Principal{UserID: 1, SessionID: coreSession.SessionID, AuthType: "session", Scopes: []string{ScopeTeamsRead}}, "fixture-request")
	if err != nil {
		t.Fatal(err)
	}
	if code := coreRequest(token); code != 200 {
		t.Fatalf("Core denied real authorized identity: %d", code)
	}
	if err := nbRepo.RevokeSession(context.Background(), coreSession.SessionID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if code := coreRequest(token); code != 401 {
		t.Fatalf("Core accepted revoked identity: %d", code)
	}
	owner := "a@example.com"
	instance := &models.Instance{UserID: 1, Owner: &owner, Name: "owned-member", Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "running", OSType: "openclaw", OSVersion: "latest", StorageClass: "standard", MountPath: "/config", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := instances.Create(instance); err != nil {
		t.Fatal(err)
	}
	member := &models.TeamMember{TeamID: aTeam.ID, UserID: 1, InstanceID: &instance.ID, MemberKey: "leader", DisplayName: "主编", Role: "leader", RuntimeType: "openclaw", InstanceMode: "lite", Status: "idle", Availability: "idle"}
	if err := repo.CreateMember(member); err != nil {
		t.Fatal(err)
	}
	if got, err := instances.(repository.IEISystemInstanceRepository).GetSupportedByOwnerEmail(owner, 0, 20); err != nil || len(got) != 0 {
		t.Fatalf("Team member leaked into ordinary list: %d %v", len(got), err)
	}
	if n, err := instances.(repository.IEISystemInstanceRepository).CountSupportedByOwnerEmail(owner); err != nil || n != 0 {
		t.Fatalf("ordinary list count mismatch: %d %v", n, err)
	}
	if got, err := instances.(repository.NorthboundInstanceOwnerRepository).GetNorthboundByUserIDAndOwner(1, owner, 0, 20); err != nil || len(got) != 0 {
		t.Fatalf("public list includes Team member: %d %v", len(got), err)
	}
	api := NewOwnerTeamAPI(session, ownerTeamTestEngine{repo: repo}, ownerTeamTestInstances{repo: instances}, users, nil, true, 1)
	if err := api.GuardInstanceMutation(instance.ID, "reset", 0); err == nil {
		t.Fatal("direct reset bypassed Team protection")
	}
	if err := api.GuardInstanceMutation(instance.ID, "delete", 0); err == nil {
		t.Fatal("direct delete bypassed Team protection")
	}
	if err := api.GuardInstanceMutation(instance.ID, "delete", aTeam.ID); err != nil {
		t.Fatal(err)
	}
	routerFor := func(identity OwnerTeamIdentity) *gin.Engine {
		r := gin.New()
		RegisterOwnerTeamRoutes(r.Group("/owner"), func() *OwnerTeamAPI { return api }, func(*gin.Context) (*OwnerTeamIdentity, bool) { return &identity, true })
		return r
	}
	request := func(r http.Handler, method, path, body, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	router := routerFor(OwnerTeamIdentity{Owner: owner, SessionID: "iei-team:test", Source: "iei"})
	if out := request(router, "GET", fmt.Sprintf("/owner/teams/%d", aTeam.ID), "", ""); out.Code != 200 || strings.Contains(out.Body.String(), "shared_pvc") || strings.Contains(out.Body.String(), "nb-a") {
		t.Fatalf("unsafe/incorrect owner details: %d %s", out.Code, out.Body.String())
	}
	if out := request(routerFor(OwnerTeamIdentity{Owner: "b@example.com", Source: "iei"}), "GET", fmt.Sprintf("/owner/teams/%d", aTeam.ID), "", ""); out.Code != 404 {
		t.Fatalf("cross-owner HTTP response: %d", out.Code)
	}
	if out := request(routerFor(OwnerTeamIdentity{Owner: owner, UserID: 1, Source: "northbound", Principal: &Principal{UserID: 1, Scopes: []string{ScopeLiteRead}}}), "GET", "/owner/teams", "", ""); out.Code != 403 {
		t.Fatalf("old key gained Team permission: %d", out.Code)
	}
	if out := request(router, "POST", "/owner/teams", `{"name":"x","owner":"b@example.com"}`, "long-enough-key"); out.Code != 422 {
		t.Fatalf("owner override accepted: %d", out.Code)
	}
	services.SetRuntimeImageSettingsProvider(northboundRuntimeImageProviderStub{images: map[string]services.RuntimeImageConfig{"openclaw:gateway": {Image: "fixture/openclaw:7.1"}}})
	t.Cleanup(func() { services.SetRuntimeImageSettingsProvider(nil) })
	body := `{"name":"我的新团队","template_key":"builtin-leader-worker","revision":1}`
	const count = 6
	results := make(chan *httptest.ResponseRecorder, count)
	var wait sync.WaitGroup
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- request(router, "POST", "/owner/teams", body, "same-create-request")
		}()
	}
	wait.Wait()
	close(results)
	var operationID string
	for out := range results {
		if out.Code != 202 {
			t.Fatalf("concurrent create failed: %d %s", out.Code, out.Body.String())
		}
		var response struct {
			Data OperationResponse `json:"data"`
		}
		if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if operationID != "" && operationID != response.Data.OperationID {
			t.Fatal("duplicate operations created")
		}
		operationID = response.Data.OperationID
		if response.Data.TeamID == nil {
			t.Fatal("Team id was stored as an instance id")
		}
	}
	if _, n, err := store.List(1, owner, "", 0, 20); err != nil || n != 2 {
		t.Fatalf("duplicate/orphan Team reservation: %d %v", n, err)
	}
	if out := request(router, "POST", "/owner/teams", strings.Replace(body, "我的新团队", "不同请求", 1), "same-create-request"); out.Code != 409 {
		t.Fatalf("idempotency conflict not rejected: %d %s", out.Code, out.Body.String())
	}
	api.CreationUserID = user.ID
	services.SetRuntimeImageSettingsProvider(northboundRuntimeImageProviderStub{})
	if out := request(router, "POST", "/owner/teams", body, "same-create-request"); out.Code != 202 || !strings.Contains(out.Body.String(), operationID) {
		t.Fatalf("account/image change reinterpreted an existing request: %d %s", out.Code, out.Body.String())
	}
	api.CreationUserID = 1
	if out := request(routerFor(OwnerTeamIdentity{Owner: "b@example.com", SessionID: "iei-team:test", Source: "iei"}), "GET", "/owner/team-operations/"+operationID, "", ""); out.Code != 404 {
		t.Fatal("operation leaked across owner")
	}
	if out := request(routerFor(OwnerTeamIdentity{Owner: owner, SessionID: "iei-team:other", Source: "iei"}), "GET", "/owner/team-operations/"+operationID, "", ""); out.Code != 404 {
		t.Fatal("operation leaked across IEI sessions")
	}
	if start, err := store.StartStep(aTeam.ID, "member:leader", "op-test"); err != nil || !start {
		t.Fatal(err)
	}
	if _, err := store.StartStep(aTeam.ID, "member:leader", "op-test"); err == nil {
		t.Fatal("uncertain side effect was repeated")
	}
	if err := store.CompleteStep(aTeam.ID, "member:leader", "op-test"); err != nil {
		t.Fatal(err)
	}
	if start, err := store.StartStep(aTeam.ID, "member:leader", "op-test"); err != nil || start {
		t.Fatal("completed step repeated")
	}
	if err := store.CompleteStep(aTeam.ID, "member:leader", "wrong-operation"); err == nil {
		t.Fatal("foreign operation completed step")
	}
	if err := session.Collection("northbound_operations").Find(db.Cond{"operation_id": operationID}).Update(map[string]interface{}{"status": "failed", "attempt_count": 1}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if out := request(router, "POST", "/owner/team-operations/"+operationID+"/retry", `{"expected_attempt_count":1}`, ""); out.Code != 200 {
			t.Fatalf("retry/replay failed: %d %s", out.Code, out.Body.String())
		}
	}
	entered, release, dispatchDone, deleteDone := make(chan struct{}), make(chan struct{}), make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
	api.teams = ownerTeamTestEngine{repo: repo, dispatch: func(uid, id int, req services.DispatchTeamTaskRequest) (*services.TeamTaskPayload, error) {
		close(entered)
		<-release
		raw, _ := json.Marshal(req.Payload)
		task := &models.TeamTask{TeamID: id, TargetMemberID: member.ID, MessageID: req.MessageID, Status: "running", WorkflowState: "planning", PayloadJSON: string(raw), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		err := repo.CreateTask(task)
		return &services.TeamTaskPayload{TeamTask: *task}, err
	}}
	go func() {
		dispatchDone <- request(router, "POST", fmt.Sprintf("/owner/teams/%d/tasks", aTeam.ID), `{"message_id":"admission-race","payload":{"prompt":"fixture"}}`, "")
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch never entered")
	}
	go func() {
		deleteDone <- request(router, "DELETE", fmt.Sprintf("/owner/teams/%d", aTeam.ID), `{"confirm_data_loss":true}`, "admission-delete-request")
	}()
	select {
	case result := <-deleteDone:
		close(release)
		t.Fatalf("delete escaped active dispatch admission: %d", result.Code)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case out := <-dispatchDone:
		if out.Code != 200 {
			t.Fatalf("admitted dispatch: %d %s", out.Code, out.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch/admission deadlocked")
	}
	select {
	case out := <-deleteDone:
		if out.Code != 409 {
			t.Fatalf("delete ignored committed task: %d", out.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("delete/admission deadlocked")
	}
	if err := store.RevokeIEISession("fixture-session", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	otherStore := repository.NewOwnerTeamRepository(session)
	if revoked, err := otherStore.IsIEISessionRevoked("fixture-session"); err != nil || !revoked {
		t.Fatal("cross-replica revocation missing")
	}
}

func TestOwnerTeamMySQLMigrationPreservesPopulatedLegacyAndResumesDDL(t *testing.T) {
	session := ownerTeamMySQL(t)
	if _, err := session.SQL().Exec(`INSERT INTO teams(user_id,name,status) VALUES(1,'retained-legacy','running')`); err != nil {
		t.Fatal(err)
	}
	if _, err := session.SQL().Exec(`ALTER TABLE teams DROP INDEX idx_teams_provider_owner, DROP INDEX idx_teams_owner_scope, DROP COLUMN resource_scope, DROP COLUMN owner, DROP COLUMN display_name`); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile("../db/migrations/077_add_northbound_owner_teams.sql")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := session.Driver().(*sql.DB).Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for pass := 0; pass < 2; pass++ {
		for _, statement := range strings.Split(string(content), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := conn.ExecContext(context.Background(), statement); err != nil {
				t.Fatalf("migration pass %d: %v", pass, err)
			}
		}
	}
	var scope, name string
	var owner sql.NullString
	if err := conn.QueryRowContext(context.Background(), `SELECT resource_scope,owner,name FROM teams WHERE name='retained-legacy'`).Scan(&scope, &owner, &name); err != nil || scope != "cm" || owner.Valid || name != "retained-legacy" {
		t.Fatalf("legacy data changed: %v", err)
	}
}

func TestOwnerTeamMySQLTemplateScopesCASAndOperationProof(t *testing.T) {
	session := ownerTeamMySQL(t)
	a, _ := repository.NewOwnerTeamTemplateRepository(session, "A@example.com")
	b, _ := repository.NewOwnerTeamTemplateRepository(session, "b@example.com")
	legacy := repository.NewCustomTeamTemplateRepository(session)
	spec := teamtemplate.TemplateSpec{SchemaVersion: 1, Name: "same name", ResolvedMemberCount: 2, Members: []teamtemplate.MemberSpec{{MemberID: "leader", DisplayName: "Leader", IsLeader: true, Role: "leader"}, {MemberID: "worker", DisplayName: "Worker", Role: "worker"}}}
	raw, _ := json.Marshal(spec)
	item := &models.CustomTeamTemplate{UserID: 1, Name: "same name", Intent: "test", ResolvedMemberCount: 2, SpecJSON: string(raw), Revision: 1}
	if err := a.Create(item); err != nil {
		t.Fatal(err)
	}
	other := &models.CustomTeamTemplate{UserID: 1, Name: "same name", Intent: "test", ResolvedMemberCount: 2, SpecJSON: string(raw), Revision: 1}
	if err := b.Create(other); err != nil {
		t.Fatal(err)
	}
	if items, err := legacy.ListByUserID(1); err != nil || len(items) != 0 {
		t.Fatal("legacy templates include owner-private templates")
	}
	if got, err := b.GetByUserIDAndID(1, item.ID); err != nil || got != nil {
		t.Fatal("cross-owner template accepted")
	}
	if err := b.Delete(1, item.ID); err == nil {
		t.Fatal("cross-owner template deletion accepted")
	}
	first, _ := a.GetByUserIDAndID(1, item.ID)
	second, _ := a.GetByUserIDAndID(1, item.ID)
	first.Revision = 2
	first.Name = "new"
	if err := a.Update(first); err != nil {
		t.Fatal(err)
	}
	second.Revision = 2
	if err := a.Update(second); err == nil {
		t.Fatal("concurrent revision overwritten")
	}
	owner := "a@example.com"
	api := NewOwnerTeamAPI(session, nil, nil, nil, nil, true, 1)
	if _, err := api.executeTemplate(context.Background(), &models.NorthboundOperation{UserID: 1, Owner: &owner, OperationID: "uncertain-model", AttemptCount: 2}, ownerTeamStoredRequest{Action: "templates.generate", Body: json.RawMessage(`{"intent":"test"}`)}); err == nil {
		t.Fatal("uncertain paid call was replayed")
	}
	proof, _ := repository.NewOwnerTeamTemplateOperationRepository(session, owner, "committed-model")
	committed := &models.CustomTeamTemplate{UserID: 1, Name: "proof", Intent: "test", ResolvedMemberCount: 2, SpecJSON: string(raw), Revision: 1}
	if err := proof.Create(committed); err != nil {
		t.Fatal(err)
	}
	got, err := api.executeTemplate(context.Background(), &models.NorthboundOperation{UserID: 1, Owner: &owner, OperationID: "committed-model", AttemptCount: 2}, ownerTeamStoredRequest{Action: "templates.generate"})
	if err != nil || got == nil || *got != committed.ID {
		t.Fatalf("lost response did not reconcile committed model result: %v", err)
	}
}

func TestOwnerTeamDTOIsBusinessOnly(t *testing.T) {
	input := map[string]interface{}{"summary": "public result", "thinking": "private reasoning", "metadata": map[string]interface{}{"redis_url": "secret"}, "payload": map[string]interface{}{"resultMarkdown": "answer", "token": "secret", "systemPrompt": "private"}, "shared_pvc_name": "private"}
	raw, _ := json.Marshal(safeOwnerTeamValue(input))
	if !bytes.Contains(raw, []byte("public result")) || bytes.Contains(raw, []byte("secret")) || bytes.Contains(raw, []byte("private")) {
		t.Fatalf("unsafe DTO: %s", raw)
	}
}
