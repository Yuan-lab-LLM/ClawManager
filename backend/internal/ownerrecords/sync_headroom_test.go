package ownerrecords

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncProductionHeadroom(t *testing.T) {
	if scanSelectionGroupBudget != 10*time.Second || nativeReadBudget != 30*time.Second || nativeScanBudget != 90*time.Second {
		t.Fatal("selection, child read and scan phase must have coordinated production headroom", scanSelectionGroupBudget, nativeReadBudget, nativeScanBudget)
	}
	if syncSourceBudget != 45*time.Second {
		t.Fatal("live source and backlog share too little time", syncSourceBudget)
	}
}

func TestEmptyProbeRetainsReaderBusyReason(t *testing.T) {
	root := t.TempDir()
	s := &Store{HistoryRoot: root}
	i := Identity{Scope: 1, Instance: 1, Runtime: "openclaw"}
	item := scanItem{identity: i, workspace: filepath.Join(root, "openclaw/user-1/instance-1")}
	nativeReaderSlots <- struct{}{}
	defer func() { <-nativeReaderSlots }()
	empty, err := s.noHistoryYet(context.Background(), item)
	if empty || !errors.Is(err, errReaderBusy) {
		t.Fatal("empty probe disguised a busy/blocked reader as a missing source", empty, err)
	}
}

func TestScanAdmissionReservesCompleteReadAndCheckpoint(t *testing.T) {
	for _, duration := range []time.Duration{time.Second, 20 * time.Second} {
		ctx, cancel := context.WithTimeout(context.Background(), duration)
		_, release, admitted := scanItemContext(ctx)
		release()
		cancel()
		if admitted {
			t.Fatal("insufficient phase remainder was turned into a doomed instance read", duration)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	work, release, admitted := scanItemContext(ctx)
	defer release()
	deadline, ok := work.Deadline()
	if !admitted || !ok || time.Until(deadline) > nativeItemBudget || time.Until(deadline) < nativeItemBudget-time.Second {
		t.Fatal("one instance can consume the full scan phase", admitted, deadline)
	}
}

func TestReadTimeoutBackoffIsShorterThanPermanentFailures(t *testing.T) {
	for n, minutes := range []time.Duration{10, 10, 20, 30, 30, 30, 30} {
		for _, err := range []error{context.DeadlineExceeded, os.ErrDeadlineExceeded} {
			if got := scanReadRetryDelay(uint64(n), err); got != minutes*time.Minute {
				t.Fatal("temporary timeout retained a long old retry", n, got)
			}
		}
		if got := scanReadRetryDelay(uint64(n), os.ErrPermission); got != scanRetryDelay(uint64(n)) {
			t.Fatal("persistent permission failure became a frequent retry", n, got)
		}
	}
}

func TestDiagnosticBudgetReservesFinalAndNeverWaitsIndefinitely(t *testing.T) {
	if diagnosticWriteAllowance(0, false) != time.Second || diagnosticWriteAllowance(4*time.Second, false) != 0 || diagnosticWriteAllowance(4*time.Second, true) != time.Second || diagnosticWriteAllowance(5*time.Second, true) != 0 {
		t.Fatal("diagnostic budget is not shared, bounded and final-reserved")
	}
	// Startup plus every serial phase and diagnostic allowance fit comfortably
	// inside one ten-minute cadence. This is not an extra catch-up worker.
	nominal := 3*syncPhaseBudget + syncSourceBudget + 2*nativeScanBudget + syncRepairBudget + syncProjectionBudget + syncReportBudget + syncDiagnosticBudget
	if nominal >= syncBatchDisplayWindow || syncBatchDisplayWindow >= summaryInterval {
		t.Fatal("relaxed timeouts can hide a stalled batch or routinely overrun the cadence", nominal)
	}
}
