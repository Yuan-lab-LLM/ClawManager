package services

import "testing"

func TestTeamNativeNFSUsesInstancePrimaryGroupAndRejectsEscapes(t *testing.T) {
	r := teamNativeRequest{Root: "/workspaces", RuntimeType: "openclaw", UserID: 1, InstanceID: 2, Action: "identity", Identity: map[string]string{teamAgentsFileName: "managed"}}
	uid, gid, path, err := validateTeamNativeRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	if uid != RuntimeLinuxID(2) || gid != uid || path != "/workspaces/openclaw/user-1/instance-2" {
		t.Fatalf("unexpected private grant: %d %d %s", uid, gid, path)
	}
	r.Context = map[string]string{"../another-instance": "bad"}
	if _, _, _, err = validateTeamNativeRequest(r); err == nil {
		t.Fatal("arbitrary context target accepted")
	}
	r = teamNativeRequest{Root: "/workspaces", UserID: 1, TeamID: 7, Action: "shared", Directories: []string{"../another-team"}}
	if _, _, _, err = validateTeamNativeRequest(r); err == nil {
		t.Fatal("Team directory escaped grant")
	}
}
