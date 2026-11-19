package systembackup

import (
	"context"
	"encoding/json"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Team copies omit the global observability journal, but Team history and
// deleted-Team Redis archive receipts still require the same immutable key.
func freezeTeamHistoryKey(ctx context.Context, source *CaptureSources) error {
	var exists int
	if err := source.MySQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='history_archive_records'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	var needed bool
	if err := source.MySQL.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM history_archive_records WHERE kind IN ('team_tasks','team_events')) OR EXISTS(SELECT 1 FROM team_redis_archive_receipts) OR EXISTS(SELECT 1 FROM team_redis_dlq_receipts)`).Scan(&needed); err != nil {
		return err
	}
	if !needed {
		return nil
	}
	if source.SecretClient == nil {
		return errors.New("Team archived history requires the immutable encryption key")
	}
	object, err := source.SecretClient.Resource(schema.GroupVersionResource{Version: "v1", Resource: "secrets"}).Namespace(source.Namespace).Get(ctx, observationKeySecret, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if object.GetUID() == "" || object.GetDeletionTimestamp() != nil {
		return errors.New("history master key identity unavailable")
	}
	raw, err := json.Marshal(object.Object)
	if err != nil {
		return err
	}
	key, err := historyMasterKey(raw)
	if err != nil {
		return err
	}
	clear(key)
	source.Secrets = append(source.Secrets, KubernetesSelection{Version: "v1", Resource: "secrets", Name: observationKeySecret, UID: string(object.GetUID())})
	return nil
}
