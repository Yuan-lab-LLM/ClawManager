package systembackuptrial

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestHistorySourceRequiresTheRegisteredInstance(t *testing.T) {
	_, profile, file := trialEnvironmentFixture(t)
	profile.Side = "source"
	profile.Source.WorkspaceClaim = "hermes-snapshot"
	profile.Source.HermesHistory = &trialHermesPlacement{
		historySourceBinding: historySourceBinding{UserID: 7, InstanceID: 13},
		Image:                profile.Source.Image, ScriptConfigMap: "history-source-v1", RunAsUser: 200013,
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{
		"ENVIRONMENT_PROFILE_FILE":          file,
		"HERMES_HISTORY_SOURCE_SOCKET":      filepath.Join(t.TempDir(), "history.sock"),
		"HERMES_HISTORY_SOURCE_ROOT":        filepath.Join(t.TempDir(), "workspaces"),
		"HERMES_HISTORY_SOURCE_USER_ID":     strconv.Itoa(profile.Source.HermesHistory.UserID),
		"HERMES_HISTORY_SOURCE_INSTANCE_ID": strconv.Itoa(profile.Source.HermesHistory.InstanceID),
	}
	service := New(func(key string) string { return settings[key[len("SYSTEM_BACKUP_TRIAL_"):]] })
	source, err := service.historySourceConfig()
	if err != nil || source.binding != profile.Source.HermesHistory.historySourceBinding {
		t.Fatalf("registered source rejected: %v", err)
	}
	settings["HERMES_HISTORY_SOURCE_INSTANCE_ID"] = "14"
	if _, err := service.historySourceConfig(); err == nil {
		t.Fatal("capture allowed a different instance")
	}
}

func TestCapturedHistoryMustIncludeTheRequestedSessions(t *testing.T) {
	selection := historySessionSelection{historySourceBinding: historySourceBinding{UserID: 7, InstanceID: 13}, SessionIDs: []string{"child"}}
	for _, test := range []struct {
		payload string
		want    bool
	}{
		{`{"user_id":7,"instance_id":13,"sessions":[{"id":"parent"},{"id":"child"}]}`, true},
		{`{"user_id":7,"instance_id":13,"sessions":[{"id":"parent"}]}`, false},
		{`{"user_id":7,"instance_id":14,"sessions":[{"id":"child"}]}`, false},
	} {
		if capturedHistoryIncludes([]byte(test.payload), selection) != test.want {
			t.Fatal("incorrect capture selection verification")
		}
	}
}
