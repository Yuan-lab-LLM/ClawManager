//go:build !linux

package ownerrecords

// Windows fixtures are not evidence of a production NFS filesystem contract.
func openClawSQLiteNetworkFilesystem(string) (bool, error) { return false, nil }
