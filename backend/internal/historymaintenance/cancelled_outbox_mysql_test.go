package historymaintenance

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func TestCancelledOutboxTickRequiresExactReceiptIdentity(t *testing.T) {
	for _, tc := range []struct{ name, change string }{
		{"source case", "UPDATE history_outbox_cancellations SET source_event_id='SOURCE'"},
		{"destination case", "UPDATE history_outbox_cancellations SET destination='WORKER'"},
		{"message case", "UPDATE history_outbox_cancellations SET message_id='MESSAGE'"},
		{"payload digest case", "UPDATE history_outbox_cancellations SET payload_sha256=UPPER(payload_sha256)"},
		{"receipt closure digest case", "UPDATE history_outbox_cancellations SET closure_sha256=UPPER(closure_sha256)"},
		{"native closure digest case", "UPDATE history_replay_closures SET proof_sha256=UPPER(proof_sha256)"},
		{"both closure digests uppercase", "UPDATE history_outbox_cancellations SET closure_sha256=UPPER(closure_sha256); UPDATE history_replay_closures SET proof_sha256=UPPER(proof_sha256)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := fixture(t)
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte("{}")))
			proof := strings.Repeat("a", 64)
			exec(t, db, `INSERT INTO teams VALUES(1,1,'deleted')`)
			exec(t, db, `INSERT INTO team_event_outbox VALUES(1,1,'source','worker','message','{}','cancelled',NULL,UTC_TIMESTAMP(6)-INTERVAL 10 DAY)`)
			exec(t, db, `INSERT INTO history_outbox_cancellations VALUES(1,'worker','message','source',?,UTC_TIMESTAMP(6)-INTERVAL 8 DAY,'native cancellation',?)`, digest, proof)
			exec(t, db, `INSERT INTO history_replay_closures VALUES(1,UTC_TIMESTAMP(6)-INTERVAL 7 DAY,TRUE,TRUE,TRUE,?,UTC_TIMESTAMP(6))`, proof)
			exec(t, db, tc.change)
			svc := New(db, nil, nil)
			result, err := svc.Tick(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.Deleted != 0 || count(t, db, "team_event_outbox") != 1 {
				t.Errorf("case-only %s mismatch authorized payload deletion: %+v", tc.name, result)
			}
			if count(t, db, "history_outbox_cancellations") != 1 || count(t, db, "history_outbox_receipts") != 0 {
				t.Fatal("retention changed permanent cancellation/delivery facts")
			}
			exec(t, db, `UPDATE history_outbox_cancellations SET source_event_id='source',destination='worker',message_id='message',payload_sha256=?,closure_sha256=?`, digest, proof)
			exec(t, db, `UPDATE history_replay_closures SET proof_sha256=?`, proof)
			result, err = svc.Tick(context.Background())
			if err != nil || result.Deleted != 1 || count(t, db, "team_event_outbox") != 0 {
				t.Errorf("restored exact receipt failed to release payload: %+v %v", result, err)
			}
			if count(t, db, "history_outbox_cancellations") != 1 || count(t, db, "history_outbox_receipts") != 0 {
				t.Fatal("exact receipt reclamation changed permanent cancellation/delivery facts")
			}
		})
	}
}
