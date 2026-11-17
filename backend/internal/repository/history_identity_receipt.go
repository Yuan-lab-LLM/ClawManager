package repository

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/upper/db/v4"
)

var ErrArchivedTeamTask = errors.New("Team task is terminal and archived")

// Caller holds the Team row lock shared with archive/removal transactions.
func archivedTeamIdentity(sess db.Session, kind string, teamID int, keys map[string]string, payload string) (int, bool, error) {
	digest := sha256.Sum256([]byte(payload))
	var found int
	for keyType, key := range keys {
		if key == "" {
			continue
		}
		identity := sha256.Sum256([]byte(key))
		row, err := sess.SQL().QueryRow(`SELECT original_id,payload_sha256 FROM history_identity_receipts WHERE kind=? AND team_id=? AND key_type=? AND key_hash=UNHEX(?)`, kind, teamID, keyType, hex.EncodeToString(identity[:]))
		if err != nil {
			return 0, false, err
		}
		var id int
		var expected string
		err = row.Scan(&id, &expected)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, false, err
		}
		if expected != hex.EncodeToString(digest[:]) || (found != 0 && found != id) {
			return 0, false, fmt.Errorf("archived Team business identity reused with different payload")
		}
		found = id
	}
	return found, found > 0, nil
}
func identityString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
