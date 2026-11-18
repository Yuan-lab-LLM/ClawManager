package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	cmk8s "clawreef/internal/services/k8s"
	"errors"
	"k8s.io/client-go/kubernetes/fake"
	"os"
	"strings"
	"testing"
)

func TestNFSTeamWriteScopesExcludeStoppedAndClosedGenerations(t *testing.T) {
	sess := upgradeFlowDB(t)
	execute := func(query string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	execute(`ALTER TABLE teams ADD user_id INT`)
	execute(`ALTER TABLE team_members ADD team_id INT,ADD instance_id INT,ADD status VARCHAR(32)`)
	migration, err := os.ReadFile("../db/migrations/099_add_workspace_write_fences.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(statement) != "" {
			execute(statement)
		}
	}
	execute(`INSERT INTO teams(id,user_id)VALUES(7,3)`)
	for _, member := range []struct {
		id            int
		status, fence string
		generation    int
	}{{12, "running", "open", 4}, {13, "stopped", "closed", 4}, {14, "creating", "", 4}, {15, "running", "closed", 4}, {16, "running", "open", 3}, {17, "creating", "open", 4}} {
		execute(`INSERT INTO instances(id,user_id,status,runtime_generation)VALUES(?,3,?,4)`, member.id, member.status)
		execute(`INSERT INTO team_members(id,team_id,instance_id,status)VALUES(?,7,?,'idle')`, member.id, member.id)
		if member.fence != "" {
			execute(`INSERT INTO instance_workspace_write_fences(instance_id,owner_user_id,generation,pod_uid,state,updated_at)VALUES(?,3,?,'pod',?,NOW())`, member.id, member.generation, member.fence)
		}
	}
	service := &RuntimeUpgradeService{sess: sess}
	members, err := service.WorkspaceTeamWriteScopes(t.Context(), WorkspaceFileScope{TeamID: 7, UserID: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].InstanceID != 12 || members[1].InstanceID != 17 {
		t.Fatalf("inactive or closed member blocked authorized shared writer: %+v", members)
	}
}

func TestNFSTeamWriterPersistsAcrossRepositoryRestartWithoutMembers(t *testing.T) {
	sess := upgradeFlowDB(t)
	run := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	run(`ALTER TABLE teams ADD user_id INT,ADD status VARCHAR(32)`)
	migration, err := os.ReadFile("../db/migrations/100_add_nfs_external_writer_tracking.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(q) != "" {
			run(q)
		}
	}
	run(`INSERT INTO teams(id,user_id,status)VALUES(7,3,'active')`)
	intent := models.WorkspaceWriteIntent{Operation: strings.Repeat("a", 32), TeamID: 7, OwnerID: 3, Action: "upload"}
	if err := repository.NewWorkspaceWriteRepository(sess).Begin(t.Context(), intent); err != nil {
		t.Fatal(err)
	}
	restarted := repository.NewWorkspaceWriteRepository(sess)
	other := intent
	other.Operation = strings.Repeat("b", 32)
	if err := restarted.Begin(t.Context(), other); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatalf("restart forgot unknown Team writer: %v", err)
	}
	if err := restarted.Complete(t.Context(), intent); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Begin(t.Context(), other); err != nil {
		t.Fatal(err)
	}
}

func TestNFSBrowserRegistrySurvivesRepositoryRestart(t *testing.T) {
	sess := upgradeFlowDB(t)
	migration, err := os.ReadFile("../db/migrations/100_add_nfs_external_writer_tracking.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err := sess.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	repo := repository.NewBrowserWorkerRepository(sess).(repository.BrowserWriterRegistry)
	raw := `{"Admitted":true,"DeploymentUID":"old","Pods":{}}`
	if err := repo.StoreBrowserWriter(t.Context(), 12, 0, raw); err != nil {
		t.Fatal(err)
	}
	restarted := repository.NewBrowserWorkerRepository(sess).(repository.BrowserWriterRegistry)
	got, revision, err := restarted.LoadBrowserWriter(t.Context(), 12)
	if err != nil || got != raw || revision != 1 {
		t.Fatalf("lost admission: %s %d %v", got, revision, err)
	}
	for i := 0; i < 2; i++ {
		worker := &BrowserWorkerService{repo: repository.NewBrowserWorkerRepository(sess), client: &cmk8s.Client{Clientset: fake.NewSimpleClientset(), RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
		if err := worker.DeleteForInstance(t.Context(), 12); err == nil {
			t.Fatal("missing admitted API objects released SQL writer history")
		}
	}
	if err := restarted.StoreBrowserWriter(t.Context(), 12, 0, `{}`); err == nil {
		t.Fatal("duplicate creation overwrote admission")
	}
	if err := restarted.StoreBrowserWriter(t.Context(), 12, 2, `{}`); err == nil {
		t.Fatal("stale writer overwrote registry")
	}
}
