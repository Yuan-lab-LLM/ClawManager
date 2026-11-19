package systembackup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"clawreef/internal/observability"
	"github.com/minio/minio-go/v7"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type historySourceObjects struct {
	source    CaptureSources
	locations map[string]ObjectMetadata
}

func (s historySourceObjects) PutObject(context.Context, string, []byte, string) error {
	return errors.New("history capture is read-only")
}
func (s historySourceObjects) GetObject(ctx context.Context, key string) ([]byte, error) {
	m, ok := s.locations[key]
	if !ok {
		return nil, errors.New("Team history dependency missing from source bucket")
	}
	o, err := s.source.Objects.GetObject(ctx, m.Bucket, m.Key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer o.Close()
	raw, err := io.ReadAll(io.LimitReader(o, maxRowBytes+65))
	if len(raw) > maxRowBytes+64 {
		return nil, errors.New("Team history dependency exceeds bound")
	}
	return raw, err
}

func captureTeamDataHistoryObjects(ctx context.Context, w io.Writer, source CaptureSources, b ObjectBudget) error {
	refs := []observability.BodyRef{}
	for _, query := range []string{`SELECT object_ref FROM history_archive_records WHERE kind IN ('team_events','team_tasks') LIMIT ?`, `SELECT object_ref FROM team_redis_archive_receipts LIMIT ?`, `SELECT object_ref FROM team_redis_dlq_receipts LIMIT ?`} {
		rows, err := source.MySQL.QueryContext(ctx, query, b.MaxItems+1)
		if err != nil {
			return err
		}
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return err
			}
			found, e := historyBodyRefs(raw)
			if e != nil || len(found) != 1 {
				rows.Close()
				return errors.New("Team history object reference invalid")
			}
			refs = append(refs, found...)
			if len(refs) > b.MaxItems {
				rows.Close()
				return errors.New("Team history dependencies exceed bound")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	needed := map[string]bool{}
	if len(refs) > 0 {
		var key []byte
		for _, selection := range source.Secrets {
			if selection.Name != observationKeySecret {
				continue
			}
			if source.SecretClient == nil {
				return errors.New("Team history secret client unavailable")
			}
			object, err := source.SecretClient.Resource(selection.gvr()).Namespace(source.Namespace).Get(ctx, selection.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if string(object.GetUID()) != selection.UID {
				return errors.New("Team history key identity changed")
			}
			raw, _ := json.Marshal(object.Object)
			key, err = historyMasterKey(raw)
			if err != nil {
				return err
			}
		}
		if len(key) != 32 {
			return errors.New("Team history encryption key omitted")
		}
		defer clear(key)
		locations := map[string]ObjectMetadata{}
		count := 0
		for _, bucket := range source.ObjectBuckets {
			for item := range source.Objects.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
				if item.Err != nil {
					return item.Err
				}
				count++
				if count > b.MaxItems {
					return errors.New("Team object inventory exceeds bound")
				}
				pos := strings.LastIndex(item.Key, "observability/v1/")
				if pos < 0 || (pos > 0 && item.Key[pos-1] != '/') {
					continue
				}
				logical := item.Key[pos:]
				if _, ok := locations[logical]; ok {
					return errors.New("ambiguous Team history object location")
				}
				locations[logical] = ObjectMetadata{Bucket: bucket, Key: item.Key}
			}
		}
		encrypted, err := observability.NewEncryptedObjects(historySourceObjects{source, locations}, key)
		if err != nil {
			return err
		}
		var total int64
		for i := 0; i < len(refs); i++ {
			ref := refs[i]
			if needed[ref.ObjectKey] {
				continue
			}
			plain, err := encrypted.Get(ctx, ref.Owner, ref)
			if err != nil {
				return err
			}
			total += int64(len(plain))
			if total > b.MaxBytes {
				return errors.New("Team history hydration exceeds bound")
			}
			needed[ref.ObjectKey] = true
			nested, err := historyBodyRefs(plain)
			if err != nil {
				return err
			}
			for _, child := range nested {
				if child.Owner != ref.Owner {
					return errors.New("Team nested history owner differs")
				}
				refs = append(refs, child)
			}
			if len(refs) > b.MaxItems {
				return errors.New("Team history dependencies exceed bound")
			}
		}
	}
	return captureObjectsSelected(ctx, source.Objects, w, source.ObjectBuckets, b, true, func(bucket, key string) bool {
		pos := strings.LastIndex(key, "observability/v1/")
		if pos < 0 || (pos > 0 && key[pos-1] != '/') {
			return true
		}
		return needed[key[pos:]]
	})
}
