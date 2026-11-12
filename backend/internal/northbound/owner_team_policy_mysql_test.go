package northbound

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/upper/db/v4"
)

func TestOwnerTeamCreationPolicyMySQL(t *testing.T) {
	session := ownerTeamMySQL(t)
	gin.SetMode(gin.TestMode)
	api := NewOwnerTeamAPI(session, ownerTeamTestEngine{repo: repository.NewTeamRepository(session)}, ownerTeamTestInstances{repo: repository.NewInstanceRepository(session)}, repository.NewUserRepository(session), nil, true, 1)
	owner := "policy@example.com"
	router := gin.New()
	RegisterOwnerTeamRoutes(router.Group("/owner"), func() *OwnerTeamAPI { return api }, func(*gin.Context) (*OwnerTeamIdentity, bool) {
		return &OwnerTeamIdentity{Owner: owner, SessionID: "policy-session", Source: "iei"}, true
	})
	request := func(method, path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	cap := request("GET", "/owner/team-capabilities", "", "")
	if cap.Code != 200 || strings.Contains(cap.Body.String(), `"pro"`) || !strings.Contains(cap.Body.String(), `"leader_runtime_types":["openclaw"]`) {
		t.Fatal(cap.Body.String())
	}
	for idx, members := range []string{
		`[{"member_id":"leader","runtime_type":"opencode","instance_mode":"lite"}]`,
		`[{"member_id":"leader","runtime_type":"hermes","instance_mode":"lite"}]`,
		`[{"member_id":"leader","runtime_type":"openclaw","instance_mode":"pro"}]`,
		`[{"member_id":"worker","runtime_type":"opencode","instance_mode":"pro"}]`,
	} {
		body := fmt.Sprintf(`{"name":"Rejected","template_key":"builtin-leader-worker","revision":1,"members":%s}`, members)
		w := request("POST", "/owner/teams", body, fmt.Sprintf("reject-policy-%d", idx))
		if w.Code != 422 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	// A legacy template is readable but its conflicting identity cannot reserve
	// a Team. Snapshot IDs/content/revision are not rewritten by that check.
	now := time.Now().UTC()
	item := &models.CustomTeamTemplate{UserID: 1, ResourceScope: models.TeamScopeNorthbound, Owner: &owner, Name: "Legacy", Intent: "test", Revision: 1, ResolvedMemberCount: 2, SpecJSON: `{"schemaVersion":1,"members":[{"memberId":"leader","role":"leader","isLeader":true},{"memberId":"worker","role":"team-leader","isLeader":false}]}`, CreatedAt: now, UpdatedAt: now}
	if _, err := session.Collection("custom_team_templates").Insert(item); err != nil {
		t.Fatal(err)
	}
	var stored models.CustomTeamTemplate
	if err := session.Collection("custom_team_templates").Find(db.Cond{"name": "Legacy"}).One(&stored); err != nil {
		t.Fatal(err)
	}
	w := request("POST", "/owner/teams", fmt.Sprintf(`{"name":"Custom rejected","template_id":%d,"revision":1}`, stored.ID), "reject-legacy-template")
	if w.Code != 422 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, table := range []string{"teams", "northbound_operations", "instances"} {
		n, err := session.Collection(table).Find().Count()
		if err != nil || n != 0 {
			t.Fatalf("rejected request left %s=%d: %v", table, n, err)
		}
	}
	services.SetRuntimeImageSettingsProvider(northboundRuntimeImageProviderStub{images: map[string]services.RuntimeImageConfig{"openclaw:gateway": {Image: "fixture/claw:lite"}, "opencode:gateway": {Image: "fixture/code:lite"}}})
	t.Cleanup(func() { services.SetRuntimeImageSettingsProvider(nil) })
	body := `{"name":"Valid","template_key":"builtin-leader-worker","revision":1,"members":[{"member_id":"worker","runtime_type":"opencode","instance_mode":"lite"}]}`
	w = request("POST", "/owner/teams", body, "valid-policy-create")
	if w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var response struct {
		Data OperationResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	// Convert only this disposable operation into the obsolete frozen request
	// shape to prove explicit retry cannot re-enqueue disallowed allocations.
	var frozen ownerTeamStoredRequest
	op, err := api.operations.GetOperationByID(response.Data.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(op.RequestPayload), &frozen); err != nil {
		t.Fatal(err)
	}
	frozen.TeamRequest.Members[1].InstanceMode = "pro"
	raw, _ := json.Marshal(frozen)
	if err = session.Collection("northbound_operations").Find(db.Cond{"operation_id": op.OperationID}).Update(map[string]interface{}{"status": "failed", "attempt_count": 1, "request_payload": string(raw)}); err != nil {
		t.Fatal(err)
	}
	w = request("POST", "/owner/team-operations/"+op.OperationID+"/retry", `{"expected_attempt_count":1}`, "")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "TEAM_CREATION_POLICY") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	op, err = api.operations.GetOperationByID(op.OperationID)
	if err != nil || op.Status != "failed" || op.AttemptCount != 1 {
		t.Fatalf("obsolete retry mutated operation: %+v %v", op, err)
	}
}
