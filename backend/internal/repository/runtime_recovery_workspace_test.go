package repository

import "testing"

func TestRecoveryWorkspaceIsolationMySQL(t *testing.T) {
 instances:=runtimeSecurityMySQL(t);sess:=instances.sess
 for _,q:=range []string{`CREATE TABLE team_members(instance_id INT)`,`CREATE TABLE teams(shared_mount_path TEXT)`,`INSERT INTO instances(id,workspace_path) VALUES(155,'/workspaces/opencode/user-1/instance-155')`}{if _,err:=sess.SQL().Exec(q);err!=nil{t.Fatal(err)}}
 repo:=&browserWorkerRepository{sess:sess}
 for _,tc:=range []struct{name,insert string}{
  {"none",""},
  {"membership",`INSERT INTO team_members VALUES(155)`},
  {"same-workspace",`INSERT INTO instances(id,workspace_path) VALUES(156,'/workspaces/opencode/user-1/instance-155')`},
  {"child-workspace",`INSERT INTO instances(id,workspace_path) VALUES(156,'/workspaces/opencode/user-1/instance-155/shared')`},
  {"parent-workspace",`INSERT INTO instances(id,workspace_path) VALUES(156,'/workspaces/opencode/user-1')`},
  {"team-shared",`INSERT INTO teams VALUES('/workspaces/opencode/user-1/instance-155/shared')`},
 } {t.Run(tc.name,func(t *testing.T){
  for _,q:=range []string{`DELETE FROM team_members`,`DELETE FROM teams`,`DELETE FROM instances WHERE id<>155`}{if _,err:=sess.SQL().Exec(q);err!=nil{t.Fatal(err)}}
  if tc.insert!="" {if _,err:=sess.SQL().Exec(tc.insert);err!=nil{t.Fatal(err)}}
  err:=repo.VerifyRecoveryWorkspaceIsolation(t.Context(),155);if (err==nil)!=(tc.name=="none"){t.Fatalf("%s: %v",tc.name,err)}
 })}
}
