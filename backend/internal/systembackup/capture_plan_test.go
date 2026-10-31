package systembackup

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

func TestCapturePlanSealsOnlyBothOriginalJobsAndKeepsForcedResumeHistory(t *testing.T) {
	for _, forced := range []bool{false, true} {
		name := "normal-resume"
		if forced {
			name = "forced-resume"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := Store{controlDB(t), "capture-plan"}
			if err := s.Bootstrap(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
				t.Fatal(err)
			}
			writerHash := strings.Repeat("a", 64)
			if err := s.RecordHealth(ctx, 1, "writer_registry", "healthy", "none", writerHash, 300); err != nil {
				t.Fatal(err)
			}
			x, _, err := s.Create(ctx, 1, "capture-plan-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
			if err != nil {
				t.Fatal(err)
			}
			l, err := s.AcquireLeadership(ctx, "leader", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			x, err = s.ClaimLedTask(ctx, l, x.ID, x.Version, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			x, err = s.Advance(ctx, x, l.Owner, "preparing", "none")
			if err != nil {
				t.Fatal(err)
			}
			g := Gate{s.DB}
			if err = g.EnsureScope(ctx, "installation"); err != nil {
				t.Fatal(err)
			}
			if err = g.RegisterParticipant(ctx, "installation", "runtime-1"); err != nil {
				t.Fatal(err)
			}
			r := captureFixtureRequest()
			r.Origin = s.InstallationID
			r.BackupID = x.ID
			r.Gate = GateOwnership{Scope: "installation", Owner: x.ID, Generation: 1, Token: 1}
			r.DeadlineAt = x.DeadlineAt
			r.MaxBytes = 1 << 20
			secret := r
			secret.Phase = "secret_capture"
			secret.SourceVolumes = nil
			secret.Stage = PVCIdentity{Name: "secret-stage", UID: "secret-stage-uid"}
			plan, err := s.PrepareCapturePlan(ctx, l, x, CapturePlanRequest{Ordinary: r, Secrets: secret, WriterRegistryHash: writerHash})
			if err != nil {
				t.Fatal(err)
			}
			var attempts []Attempt
			var results []CaptureJobResult
			wrapper := AESKeyWrapper{Reference: SecretRef{"kubernetes", "archive-kek", "v1", "encryption"}, Key: bytes.Repeat([]byte{8}, 32)}
			for _, request := range []CaptureJobRequest{plan.Request.Ordinary, plan.Request.Secrets} {
				a, err := s.PrepareAttempt(ctx, l, x, request.Phase, request)
				if err != nil {
					t.Fatal(err)
				}
				job, _, err := captureJob(a)
				if err != nil {
					t.Fatal(err)
				}
				job.UID = types.UID("job-" + request.Phase)
				if err = s.BindCaptureJob(ctx, l, x, a, job); err != nil {
					t.Fatal(err)
				}
				binding, err := s.CaptureBinding(ctx, a.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err = s.RecordCaptureReady(ctx, l, x, a, binding, true); err != nil {
					t.Fatal(err)
				}
				candidate := &CaptureCandidate{Schema: "system-backup-capture-candidate.v1", CapturePhaseRequest: CapturePhaseRequest{Origin: r.Origin, BackupID: x.ID, AttemptID: a.ID, Phase: request.Phase, Gate: r.Gate, StageUID: request.Stage.UID}, RegistryHash: RegistryHash(), StartedAt: time.Now().UTC(), SealedAt: time.Now().UTC()}
				for _, part := range RequiredParts {
					if (part == "secret_bundle") != (request.Phase == "secret_capture") {
						continue
					}
					p, err := CapturePart(ctx, t.TempDir(), r.Origin, x.ID, part, 1<<20, wrapper, func(_ context.Context, w io.Writer) error { _, err := w.Write([]byte("fixture")); return err })
					if err != nil {
						t.Fatal(err)
					}
					candidate.Parts = append(candidate.Parts, p)
				}
				attempts = append(attempts, a)
				results = append(results, CaptureJobResult{State: "sealed", JobUID: string(job.UID), Candidate: candidate})
			}
			for _, next := range []string{"ready_for_capture", "acquiring_gate"} {
				x, err = s.Advance(ctx, x, l.Owner, next, "none")
				if err != nil {
					t.Fatal(err)
				}
			}
			own, err := g.Begin(ctx, "installation", x.ID, RegistryHash(), time.Second, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if err = g.AckPaused(ctx, own, "runtime-1", writerHash); err != nil {
				t.Fatal(err)
			}
			own, err = g.Hold(ctx, own)
			if err != nil {
				t.Fatal(err)
			}
			x, err = s.Advance(ctx, x, l.Owner, "capturing", "none")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.SealCapturePlan(ctx, l, x); PublicError(err) != "evidence_incomplete" {
				t.Fatal("sealed incomplete capture", err)
			}
			for i, a := range attempts {
				if err = s.FinishAttempt(ctx, l, x, a, "sealed", results[i]); err != nil {
					t.Fatal(err)
				}
			}
			sealed, err := s.SealCapturePlan(ctx, l, x)
			if err != nil || sealed.Manifest == nil || sealed.Manifest.Purpose != "functional_test" || sealed.Seal == nil {
				t.Fatal(sealed, err)
			}
			if forced {
				if _, err = s.DB.Exec("UPDATE system_backup_gate SET hold_deadline=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE scope='installation'"); err != nil {
					t.Fatal(err)
				}
				if err = g.AckResumed(ctx, own, "runtime-1", true); err != nil {
					t.Fatal(err)
				}
			} else {
				if err = g.BeginRelease(ctx, own); err != nil {
					t.Fatal(err)
				}
				if err = g.AckResumed(ctx, own, "runtime-1", false); err != nil {
					t.Fatal(err)
				}
			}
			if err = g.FinishRelease(ctx, own); err != nil {
				t.Fatal(err)
			}
			observed, err := s.GetCapturePlan(ctx, x.ID)
			if err != nil || observed.SourceResumedAt == nil || (observed.ForcedResumeAt != nil) != forced {
				t.Fatal(observed, err)
			}
			if _, err = s.SealCapturePlan(ctx, l, x); forced && PublicError(err) != "quiesce_resume_failed" || !forced && err != nil {
				t.Fatal("resume history was lost", err)
			}
		})
	}
}
