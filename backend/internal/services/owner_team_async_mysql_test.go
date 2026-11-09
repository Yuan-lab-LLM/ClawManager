package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4/adapter/mysql"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// Only provision input and the remote Kubernetes/Agent boundary are fixtures.
// Team provisioning, asynchronous scheduler, SQL binding/state and readiness
// verification below are production services. Never seed members as running.
type asyncOwnerInstances struct { *instanceService; created int }
func (s *asyncOwnerInstances) ValidateCreateRequests(int, []CreateInstanceRequest) error { return nil }
func (s *asyncOwnerInstances) CreatePrevalidated(uid int, req CreateInstanceRequest) (*models.Instance,error) {
	if old,err:=s.GetByProvisioningOperationID(req.ProvisioningOperationID);err!=nil||old!=nil{return old,err}
	path:=filepath.Join(s.workspaceRoot,req.ProvisioningOperationID)
	now:=time.Now().UTC()
	i:=&models.Instance{UserID:uid,Owner:req.Owner,Name:req.Name,Type:req.Type,RuntimeType:"gateway",InstanceMode:"lite",Status:"creating",RuntimeGeneration:1,OSType:req.Type,OSVersion:"latest",StorageClass:"standard",MountPath:"/config",WorkspacePath:&path,ProvisioningOperationID:&req.ProvisioningOperationID,CreatedAt:now,UpdatedAt:now}
	if err:=s.instanceRepo.Create(i);err!=nil{return nil,err};s.created++;return i,nil
}
type asyncOwnerAgent struct{ *fakeRuntimeAgentClient; states map[string]*RuntimeAgentGatewayState; creates int }
func(a *asyncOwnerAgent) CreateGateway(_ context.Context,_ string, req RuntimeAgentCreateGatewayRequest)(*RuntimeAgentCreateGatewayResponse,error){
	a.creates++; id:=fmt.Sprintf("gw-%d-%d",req.InstanceID,req.Generation)
	a.states[id]=&RuntimeAgentGatewayState{GatewayID:id,InstanceID:req.InstanceID,Generation:req.Generation,State:"running"}
	return &RuntimeAgentCreateGatewayResponse{GatewayID:id,Port:req.GatewayPort,Status:"running"},nil
}
func(a *asyncOwnerAgent) GatewayState(_ context.Context,_,id string)(*RuntimeAgentGatewayState,error){return a.states[id],nil}

func TestOwnerTeamMySQLAsyncProvisionScheduleAndReady(t *testing.T){
	port,_:=strconv.Atoi(os.Getenv("TEST_OWNER_TEAM_MYSQL_PORT"));redis:=os.Getenv("TEST_OWNER_TEAM_REDIS_PORT")
	if port==0||redis==""{t.Skip("isolated MySQL/Redis required")}
	root,err:=mysql.Open(mysql.ConnectionURL{Host:fmt.Sprintf("127.0.0.1:%d",port),User:"root",Database:"mysql"});if err!=nil{t.Fatal(err)}
	name:=fmt.Sprintf("owner_async_%d",time.Now().UnixNano());if _,err:=root.SQL().Exec("CREATE DATABASE "+name);err!=nil{t.Fatal(err)}
	t.Cleanup(func(){root.SQL().Exec("DROP DATABASE "+name);root.Close()})
	sess,err:=appdb.Initialize(config.DatabaseConfig{Host:"127.0.0.1",Port:port,User:"root",Database:name,MaxOpenConns:8,MaxIdleConns:4});if err!=nil{t.Fatal(err)};t.Cleanup(func(){sess.Close()})
	t.Setenv("CLAWMANAGER_TEAM_REDIS_URL","redis://127.0.0.1:"+redis+"/14")
	ctx:=context.Background();work:=t.TempDir()
	report,live:=ordinaryPlacementFixture(); report.PodUID=nil // Legacy 7.1 Agent.
	report.ImageRef="registry/openclaw:team71";live.ImageRef=report.ImageRef
	labels:=map[string]string{"app":"pool"}; replicas:=int32(1)
	dep:=&appsv1.Deployment{ObjectMeta:metav1.ObjectMeta{Name:"pool",Namespace:"tenant",UID:types.UID("deployment-uid"),Labels:map[string]string{"clawmanager.io/runtime-type":"openclaw"}},Spec:appsv1.DeploymentSpec{Replicas:&replicas,Selector:&metav1.LabelSelector{MatchLabels:labels},Template:corev1.PodTemplateSpec{Spec:corev1.PodSpec{Containers:[]corev1.Container{{Name:"runtime",Image:report.ImageRef}}}}}}
	pod:=&corev1.Pod{ObjectMeta:metav1.ObjectMeta{Name:live.PodName,Namespace:"tenant",UID:types.UID(live.PodUID),Labels:labels},Spec:dep.Spec.Template.Spec,Status:corev1.PodStatus{Phase:corev1.PodRunning,PodIP:*live.PodIP,Conditions:[]corev1.PodCondition{{Type:corev1.PodReady,Status:corev1.ConditionTrue}}}}
	client:=&k8s.Client{Namespace:"tenant",Clientset:fake.NewSimpleClientset(dep,pod),WorkspaceStorageClass:"standard"}
	pods:=repository.NewRuntimePodRepository(sess);bindings:=repository.NewInstanceRuntimeBindingRepository(sess);rollouts:=repository.NewRuntimeRolloutRepository(sess)
	if err:=pods.UpsertFromAgent(ctx,&report);err!=nil{t.Fatal(err)}
	start:=time.Now().UTC().Add(-time.Minute);now:=time.Now().UTC();source:=`{"tenant/pool":"registry/openclaw:old"}`
	reset:=&models.RuntimeRollout{RuntimeType:"openclaw",TargetImageRef:"registry/openclaw:old",Phase:RuntimeUpgradePhaseEmptyPoolReset,Status:"finished",SourceImagesJSON:&source,StartedAt:&start,FinishedAt:&now}
	if err:=rollouts.Create(ctx,reset);err!=nil{t.Fatal(err)}
	legacy:=&models.RuntimeRollout{RuntimeType:"openclaw",TargetImageRef:report.ImageRef,Status:"finished",Phase:"pending",StartedAt:&start,FinishedAt:&now}
	if err:=rollouts.Create(ctx,legacy);err!=nil{t.Fatal(err)}
	agent:=&asyncOwnerAgent{fakeRuntimeAgentClient:&fakeRuntimeAgentClient{},states:map[string]*RuntimeAgentGatewayState{}}
	instances:=&asyncOwnerInstances{instanceService:&instanceService{instanceRepo:repository.NewInstanceRepository(sess),bindingRepo:bindings,runtimePodRepo:pods,agentClient:agent,podService:k8s.NewPodServiceWithClient(client),workspaceRoot:work}}
	repo:=repository.NewTeamRepository(sess);steps:=repository.NewOwnerTeamRepository(sess)
	engine:=NewTeamService(repo,instances,WithTeamKubernetesClient(client),WithTeamRuntimeWorkspaceRoot(work)).(OwnerTeamProvisioner)
	owner:="async@example.com";team:=&models.Team{UserID:1,Owner:&owner,ResourceScope:models.TeamScopeNorthbound,Name:"nb-async",Status:"creating",CommunicationMode:"leader_mediated",SharedMountPath:"/team",RedisEventsLastID:"0-0"}
	if err:=repo.CreateTeam(team);err!=nil{t.Fatal(err)}
	req:=CreateTeamRequest{Name:"async",Members:[]CreateTeamMemberRequest{{MemberID:"leader",Name:"主编",Role:"leader",IsLeader:true,RuntimeType:"openclaw",InstanceMode:"lite"},{MemberID:"worker",Name:"编辑",Role:"worker",RuntimeType:"openclaw",InstanceMode:"lite"}}}
	var pending *OwnerTeamRuntimePending
	if err:=engine.ProvisionOwnerTeam(ctx,team,req,"async-op",steps);!errors.As(err,&pending){t.Fatalf("must wait for actual scheduler: %v",err)}
	if team.Status!="creating"||agent.creates!=0{t.Fatal("provisioning falsely became ready")}
	u:=NewRuntimeUpgradeService(sess,rollouts,pods,bindings,nil,work,"")
	scheduler:=NewRuntimeScheduler(instances.instanceRepo,pods,bindings,rollouts,agent,nil,nil,k8s.NewRuntimeDeploymentService(client.Clientset),time.Second,WithRuntimeSchedulerNamespace("tenant"),WithRuntimeSchedulerWorkspaceRoot(work),WithRuntimeUpgradeService(u),WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance)(map[string]string,error){return map[string]string{},nil}))
	if err:=scheduler.reconcile(ctx);err!=nil{t.Fatal(err)}
	if err:=engine.ProvisionOwnerTeam(ctx,team,req,"async-op",steps);err!=nil{t.Fatal(err)}
	if team.Status!="running"||agent.creates!=2||instances.created!=2{t.Fatalf("not actually ready: %s gateways=%d instances=%d",team.Status,agent.creates,instances.created)}
	if err:=scheduler.reconcile(ctx);err!=nil{t.Fatal(err)}
	if err:=engine.ProvisionOwnerTeam(ctx,team,req,"async-op",steps);err!=nil{t.Fatal(err)}
	if agent.creates!=2||instances.created!=2{t.Fatal("replay duplicated instances/gateways")}
	if tasks,err:=sess.Collection("team_tasks").Find("team_id",team.ID).Count();err!=nil||tasks!=1{t.Fatal("bootstrap duplicated",err)}
}
