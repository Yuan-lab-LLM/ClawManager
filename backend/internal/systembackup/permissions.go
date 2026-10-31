package systembackup

import (
	"errors"
	"strconv"
	"strings"
)

type Permission string

const (
	Read              Permission = "system_backup.read"
	Operate           Permission = "system_backup.operate"
	Configure         Permission = "system_backup.configure"
	SecurityConfigure Permission = "system_backup.security_configure"
	Destructive       Permission = "system_backup.destructive"
)

var rolePermissions = map[string][]Permission{"viewer": {Read}, "operator": {Read, Operate}, "config_admin": {Read, Configure}, "security_admin": {Read, Operate, Configure, SecurityConfigure, Destructive}}

type RoleBindings map[int]string

func (b RoleBindings) Allows(actor int, p Permission) bool {
	for _, v := range rolePermissions[b[actor]] {
		if v == p {
			return true
		}
	}
	return false
}

// Offline bootstrap input: decimal-user-id:fixed-role, separated by commas.
// Ordinary application admin status alone never grants backup mutations.
func ParseRoleBindings(value string) (RoleBindings, error) {
	b := RoleBindings{}
	if strings.TrimSpace(value) == "" {
		return b, nil
	}
	for _, part := range strings.Split(value, ",") {
		pair := strings.Split(strings.TrimSpace(part), ":")
		if len(pair) != 2 {
			return nil, errors.New("invalid backup role binding")
		}
		id, e := strconv.Atoi(pair[0])
		if e != nil || id < 1 || rolePermissions[pair[1]] == nil || b[id] != "" {
			return nil, errors.New("invalid backup role binding")
		}
		b[id] = pair[1]
	}
	security := 0
	for _, r := range b {
		if r == "security_admin" {
			security++
		}
	}
	if security < 2 {
		return nil, errors.New("bootstrap requires at least two security administrators")
	}
	return b, nil
}
