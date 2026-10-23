package models

import (
	"encoding/hex"
	"errors"
	"time"
)

var ErrRuntimeUpgradeInProgress = errors.New("实例正在执行受保护的升级操作，暂不能启停、重启、删除、修改工作区或下发管理命令；请等待升级完成或由管理员处理恢复")
var ErrRuntimeUpgradeConflict = errors.New("runtime upgrade operation or instance identity does not match")
var ErrRuntimeUpgradeWritersActive = errors.New("实例仍有未完成的管理命令或尚未确认结束的工作区写入，不能进入升级停止阶段；请等待完成或核实写入进程后重新检查")

// Durable fence, not a lease: expiry/restarts must never silently permit writes.
type RuntimeUpgrade struct {
	InstanceID int       `db:"instance_id" json:"instance_id"`
	OwnerID    int       `db:"owner_user_id" json:"owner_user_id"`
	Generation int       `db:"generation" json:"generation"`
	Operation  string    `db:"operation_id" json:"operation_id"`
	State      string    `db:"state" json:"state"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time `db:"updated_at" json:"updated_at"`
}

func (u RuntimeUpgrade) Active() bool { return u.State != "cancelled" && u.State != "completed" }

func ValidRuntimeOperation(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 16 && value == hex.EncodeToString(b)
}
