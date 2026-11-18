package systembackup

import (
	"archive/tar"
	"context"
	"errors"
	"math"
	"path"
)

// Check the actual work volume, including block rounding and implicit parent
// directories. Remote SQL/object quotas remain failure-handled, not inferred
// from this filesystem. This is a preflight observation, not a reservation.
func (s *TeamDataService) preflightRestore(ctx context.Context, a PublishedArtifact) error {
	available, e := availableRestoreBytes(s.cfg.RestoreRoot)
	if e != nil {
		return e
	}
	return s.preflightRestoreWithSpace(ctx, a, available)
}

func (s *TeamDataService) preflightRestoreWithSpace(ctx context.Context, a PublishedArtifact, available uint64) error {
	const reserve = 64 << 20
	required := uint64(reserve)
	add := func(n uint64) error {
		if n > math.MaxUint64-required {
			return errors.New("capacity_exceeded")
		}
		required += n
		if required > available {
			return errors.New("capacity_exceeded")
		}
		return nil
	}
	if required > available {
		return errors.New("capacity_exceeded")
	}
	for _, p := range a.Manifest.Payload.Parts {
		if p.Name == "deployment_resources" || p.Name == "secret_bundle" {
			// These parts are re-encrypted into the copy's history directory.
			if p.PlaintextBytes < 0 || p.CiphertextBytes < 0 {
				return errors.New("validation_failed")
			}
			if e := add(uint64(p.PlaintextBytes) + uint64(p.CiphertextBytes)); e != nil {
				return e
			}
		}
	}
	seen := map[string]bool{}
	return validatePublishedSnapshot(ctx, s.store, a, s.wrapper, CaptureBudget{s.cfg.MaxBytes, s.cfg.MaxItems}, func(h *tar.Header) error {
		// One block for each directory/entry, plus rounded regular-file data.
		for name := h.Name; name != "."; name = path.Dir(name) {
			if !seen[name] {
				seen[name] = true
				if e := add(4096); e != nil {
					return e
				}
			}
		}
		if h.Typeflag == tar.TypeReg {
			return add((uint64(h.Size) + 4095) / 4096 * 4096)
		}
		return nil
	})
}
