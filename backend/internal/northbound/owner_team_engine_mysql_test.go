package northbound

import (
	"bufio"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"
	"context"
	"fmt"
	"github.com/upper/db/v4"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type ownerTeamEngineInstances struct {
	services.InstanceService
	repo     repository.InstanceRepository
	root     string
	loseOnce bool
	created  int
	ready    bool
}

func (s *ownerTeamEngineInstances) OwnerTeamRuntimeReady(context.Context, *models.Instance) (bool, error) {
	return s.ready, nil
}

func (s *ownerTeamEngineInstances) ValidateCreateRequests(_ int, _ []services.CreateInstanceRequest) error {
	return nil
}
func (s *ownerTeamEngineInstances) GetByID(id int) (*models.Instance, error) {
	return s.repo.GetByID(id)
}
func (s *ownerTeamEngineInstances) GetByProvisioningOperationID(id string) (*models.Instance, error) {
	return s.repo.(interface {
		GetByProvisioningOperationID(string) (*models.Instance, error)
	}).GetByProvisioningOperationID(id)
}
func (s *ownerTeamEngineInstances) CreatePrevalidated(uid int, req services.CreateInstanceRequest) (*models.Instance, error) {
	if old, err := s.GetByProvisioningOperationID(req.ProvisioningOperationID); err != nil || old != nil {
		return old, err
	}
	path := filepath.Join(s.root, req.ProvisioningOperationID)
	now := time.Now().UTC()
	id := req.ProvisioningOperationID
	item := &models.Instance{UserID: uid, Owner: req.Owner, Name: req.Name, Type: req.Type, RuntimeType: "gateway", InstanceMode: "lite", Status: "running", OSType: req.Type, OSVersion: "latest", StorageClass: "standard", MountPath: "/config", WorkspacePath: &path, ProvisioningOperationID: &id, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Create(item); err != nil {
		return nil, err
	}
	s.created++
	if s.loseOnce {
		s.loseOnce = false
		return nil, fmt.Errorf("fixture lost create response after durable instance")
	}
	return item, nil
}

func TestOwnerTeamMySQLRealEngineResumesLostCreateWithoutNewInstance(t *testing.T) {
	if os.Getenv("TEST_OWNER_TEAM_REDIS_PORT") == "" {
		t.Skip("isolated Redis required")
	}
	session := ownerTeamMySQL(t)
	repo := repository.NewTeamRepository(session)
	steps := repository.NewOwnerTeamRepository(session)
	root := t.TempDir()
	instances := &ownerTeamEngineInstances{repo: repository.NewInstanceRepository(session), root: root, loseOnce: true}
	client := &k8s.Client{Clientset: fake.NewSimpleClientset(), Namespace: "owner-fixture", WorkspaceStorageClass: "standard"}
	engine := services.NewTeamService(repo, instances, services.WithTeamKubernetesClient(client), services.WithTeamRuntimeWorkspaceRoot(root))
	redisPort := os.Getenv("TEST_OWNER_TEAM_REDIS_PORT")
	t.Setenv("CLAWMANAGER_TEAM_REDIS_URL", "redis://127.0.0.1:"+redisPort+"/15")
	owner, name := "engine@example.com", "可恢复团队"
	team := &models.Team{UserID: 1, Name: "nb-engine-test", DisplayName: &name, Owner: &owner, ResourceScope: models.TeamScopeNorthbound, Status: "creating", CommunicationMode: "leader_mediated", SharedMountPath: "/team", RedisEventsLastID: "0-0"}
	if err := repo.CreateTeam(team); err != nil {
		t.Fatal(err)
	}
	req := services.CreateTeamRequest{Name: name, CommunicationMode: "leader_mediated", Members: []services.CreateTeamMemberRequest{{MemberID: "leader", Name: "主编", Role: "leader", IsLeader: true, RuntimeType: "openclaw", InstanceMode: "lite"}, {MemberID: "worker", Name: "筛选员", Role: "worker", RuntimeType: "openclaw", InstanceMode: "lite"}}}
	provision := engine.(services.OwnerTeamProvisioner)
	if err := provision.ProvisionOwnerTeam(context.Background(), team, req, "fixture-op", steps); err == nil {
		t.Fatal("lost response fixture did not fail")
	}
	secret, err := client.Clientset.CoreV1().Secrets(client.GetNamespace(1)).Get(context.Background(), *team.TeamTokenSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	token := string(secret.Data["CLAWMANAGER_TEAM_TOKEN"])
	if err := provision.ProvisionOwnerTeam(context.Background(), team, req, "fixture-op", steps); err == nil {
		t.Fatal("resource creation alone incorrectly completed the Team")
	} else if _, ok := err.(*services.OwnerTeamRuntimePending); !ok {
		t.Fatal(err)
	}
	if n, _ := session.Collection("team_tasks").Find(db.Cond{"team_id": team.ID}).Count(); n != 0 {
		t.Fatal("bootstrap ran before runtime readiness")
	}
	instances.ready = true
	if err := provision.ProvisionOwnerTeam(context.Background(), team, req, "fixture-op", steps); err != nil {
		t.Fatal(err)
	}
	if instances.created != 2 {
		t.Fatalf("created %d instances instead of two", instances.created)
	}
	if err := provision.ProvisionOwnerTeam(context.Background(), team, req, "fixture-op", steps); err != nil {
		t.Fatal(err)
	}
	secret, err = client.Clientset.CoreV1().Secrets(client.GetNamespace(1)).Get(context.Background(), *team.TeamTokenSecretName, metav1.GetOptions{})
	if err != nil || string(secret.Data["CLAWMANAGER_TEAM_TOKEN"]) != token {
		t.Fatal("token rotated on recovery")
	}
	members, err := repo.ListMembersByTeamID(team.ID)
	if err != nil || len(members) != 2 {
		t.Fatal("duplicate members")
	}
	for _, m := range members {
		if m.InstanceID == nil {
			t.Fatal("missing binding")
		}
		instance, _ := instances.GetByID(*m.InstanceID)
		data, err := os.ReadFile(filepath.Join(*instance.WorkspacePath, "home", ".openclaw", "workspace", "AGENTS.md"))
		if err != nil || !strings.Contains(string(data), "CLAWMANAGER TEAM OVERLAY") {
			t.Fatal("identity files incomplete")
		}
		if instance.Owner == nil || *instance.Owner != owner {
			t.Fatal("member owner missing")
		}
	}
	n, err := session.Collection("team_tasks").Find(db.Cond{"team_id": team.ID}).Count()
	if err != nil || n != 1 {
		t.Fatalf("bootstrap duplicate: %d %v", n, err)
	}
	task, err := engine.DispatchTask(1, team.ID, services.DispatchTeamTaskRequest{MessageID: "owner-engine-dispatch", Payload: map[string]interface{}{"prompt": "fixture business question", "user_key": "owner-" + owner}})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "dispatched" {
		t.Fatalf("not dispatched: %s", task.Status)
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+redisPort, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	send := func(args ...string) string {
		fmt.Fprintf(conn, "*%d\r\n", len(args))
		for _, arg := range args {
			fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(arg), arg)
		}
		reply, _ := reader.ReadString('\n')
		return reply
	}
	if !strings.HasPrefix(send("SELECT", "15"), "+OK") {
		t.Fatal("Redis selection failed")
	}
	if value := send("XLEN", fmt.Sprintf("claw:team:%d:inbox:leader", team.ID)); value != ":1\r\n" {
		t.Fatalf("Redis dispatch missing or duplicated: %q", value)
	}
	if _, err := engine.DispatchTask(1, team.ID, services.DispatchTeamTaskRequest{MessageID: "owner-engine-dispatch", Payload: map[string]interface{}{"prompt": "fixture business question", "user_key": "owner-" + owner}}); err != nil {
		t.Fatal(err)
	}
	if value := send("XLEN", fmt.Sprintf("claw:team:%d:inbox:leader", team.ID)); value != ":1\r\n" {
		t.Fatal("dispatch replay duplicated Redis delivery")
	}
	// Only these fixture keys are removed from the disposable local Redis.
	send("DEL", fmt.Sprintf("claw:team:%d:inbox:leader", team.ID))
}
