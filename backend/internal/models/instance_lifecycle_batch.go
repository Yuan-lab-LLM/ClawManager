package models

import "time"

type InstanceLifecycleBatch struct {
	BatchID       string    `db:"batch_id" json:"batch_id"`
	UserID        int       `db:"user_id" json:"-"`
	Action        string    `db:"action" json:"action"`
	Status        string    `db:"status" json:"status"`
	PolicyVersion int       `db:"policy_version" json:"policy_version"`
	RetryRound    int       `db:"retry_round" json:"retry_round"`
	RequestHash   string    `db:"request_hash" json:"-"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time `db:"updated_at" json:"updated_at"`
}

type InstanceLifecycleBatchItem struct {
	ID           int64  `db:"id,primarykey,autoincrement" json:"id"`
	BatchID      string `db:"batch_id" json:"batch_id"`
	SourceID     int    `db:"source_id" json:"source_id"`
	SourceName   string `db:"source_name" json:"source_name"`
	RuntimeType  string `db:"runtime_type" json:"runtime_type"`
	OperationID  string `db:"operation_id" json:"operation_id"`
	RuntimePodID int64  `db:"runtime_pod_id" json:"-"`
	State        string `db:"state" json:"state"`
}
