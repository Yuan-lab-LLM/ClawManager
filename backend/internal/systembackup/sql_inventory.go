package systembackup

import (
	"errors"
	"io"
)

// ValidateBusinessSQLSnapshot is the system-restore inventory check, distinct
// from the reusable single-table parser/importer. Empty tables must still be
// present; a valid signature and individual table hashes do not prove coverage.
func ValidateBusinessSQLSnapshot(r io.Reader) error {
	result, e := ReadSQLSnapshot(r, nil, nil)
	if e != nil {
		return e
	}
	return requireBusinessSQLInventory(result)
}
func requireBusinessSQLInventory(result SQLResult) error {
	expected := 0
	for _, policy := range Registry() {
		if policy.BackupStrategy != "mysql_dump" {
			continue
		}
		expected++
		if _, ok := result.Tables[policy.Name]; !ok {
			return errors.New("incomplete business SQL inventory")
		}
	}
	monthly, err := monthlyManifest(result.MonthlyCatalog)
	if err != nil {
		return err
	}
	for name := range monthly {
		if _, ok := result.Tables[name]; !ok {
			return errors.New("monthly inventory missing")
		}
	}
	if len(result.Tables) != expected+len(monthly) {
		return errors.New("unexpected business SQL inventory")
	}
	return nil
}
