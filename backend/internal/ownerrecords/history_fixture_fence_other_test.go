//go:build !linux

package ownerrecords

import "fmt"

func lockOwnedOpenClawHistoryFixture(string) (func(), error) {
	return nil, fmt.Errorf("actual fixture writer closure requires Linux")
}
