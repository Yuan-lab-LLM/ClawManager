package models

// An intent is removed only after a trusted completion (or proof of no dispatch).
// Absence of a response, age, or a new runtime generation is NOT completion.
type WorkspaceWriteIntent struct {
	Operation  string
	InstanceID int
	OwnerID    int
	Generation int
	Action     string
}

func (w WorkspaceWriteIntent) Valid() bool {
	if !ValidRuntimeOperation(w.Operation) || w.InstanceID <= 0 || w.OwnerID <= 0 || w.Generation < 0 {
		return false
	}
	switch w.Action {
	case "upload", "mkdir", "rename", "delete":
		return true
	}
	return false
}
