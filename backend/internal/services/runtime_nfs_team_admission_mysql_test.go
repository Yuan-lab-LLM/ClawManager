package services

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"github.com/upper/db/v4"
	"os"
	"strings"
	"testing"
)

func TestNFSUpgradeScopeRejectsUnknownTeamWriter(t *testing.T) {
	session := upgradeFlowDB(t)
	ctx := context.Background()
	run := func(query string) {
		t.Helper()
		if _, err := session.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	run(`ALTER TABLE team_members ADD team_id INT,ADD instance_id INT`)
	raw, err := os.ReadFile("../db/migrations/100_add_nfs_external_writer_tracking.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(query) != "" {
			run(query)
		}
	}
	run(`INSERT INTO instances(id,user_id,type,runtime_generation,status)VALUES(7,3,'openclaw',2,'running')`)
	run(`INSERT INTO teams(id)VALUES(11)`)
	run(`INSERT INTO team_members(id,team_id,instance_id)VALUES(1,11,7)`)
	run(`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase,batch_size)VALUES(1,'openclaw','fixture','preflight_passed','preflight',1)`)
	run(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,created_at,updated_at)VALUES(1,7,'prepared',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`)
	run(`INSERT INTO team_workspace_write_intents(operation_id,team_id,owner_user_id,action,created_at)VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',11,3,'edit',UTC_TIMESTAMP(6))`)
	service := &RuntimeUpgradeService{sess: session, nfsIOProfile: "nfs-rootless-v1"}
	check := func() error {
		return session.TxContext(ctx, func(tx db.Session) error { return service.lockUpgradeScope(ctx, tx, 1) }, nil)
	}
	if err := check(); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatalf("unknown shared writer admitted rollout: %v", err)
	}
	run(`DELETE FROM team_workspace_write_intents WHERE operation_id='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'`)
	if err := check(); err != nil {
		t.Fatal(err)
	}
}
