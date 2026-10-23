package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/upper/db/v4"
)

const MaxGatewayImageArtifactBytes = 16 << 20
const maxStoredImageArtifactBytes = MaxGatewayImageArtifactBytes + 64

var ErrImageArtifactUnavailable = errors.New("image artifact unavailable or expired")
var imageArtifactIDPattern = regexp.MustCompile(`^img_[a-f0-9]{32}$`)

type ImageArtifactOwner struct {
	UserID                   int
	InstanceID, GatewayKeyID *int
}

type ImageArtifact struct {
	ID        string
	ModelID   int
	MediaType string
	Data      []byte
}

// upper/db's MySQL converter treats plain []byte parameters as JSON. Use a
// struct Valuer to preserve the exact binary key and redact SQL error logs.
type imageSQLKey struct{ bytes [32]byte }

func (k imageSQLKey) Value() (driver.Value, error) { return k.bytes[:], nil }
func (imageSQLKey) String() string                 { return "[redacted image key]" }
func (imageSQLKey) GoString() string               { return "[redacted image key]" }

type GatewayImageArtifactService interface {
	Save(context.Context, ImageArtifactOwner, int, string, string, []byte) (string, error)
	Load(context.Context, ImageArtifactOwner, string) (*ImageArtifact, error)
}

// Deliberately separate from the legacy skill object-store interface. Image
// storage requires bounded reads and deletion; older stores fail closed.
type imageObjectStorage interface {
	ObjectStorageService
	GetObjectLimited(context.Context, string, int64) ([]byte, error)
	DeleteObject(context.Context, string) error
}

type gatewayImageArtifactService struct {
	sess    db.Session
	storage imageObjectStorage
}

func NewGatewayImageArtifactService(sess db.Session, storage ObjectStorageService) (*gatewayImageArtifactService, error) {
	safe, ok := storage.(imageObjectStorage)
	if !ok || sess == nil {
		return nil, errors.New("image artifact storage unavailable")
	}
	return &gatewayImageArtifactService{sess: sess, storage: safe}, nil
}

func imageArtifactKey(id string) (string, error) {
	if !imageArtifactIDPattern.MatchString(id) {
		return "", ErrImageArtifactUnavailable
	}
	return "gateway-images/" + id, nil
}

func (s *gatewayImageArtifactService) Save(ctx context.Context, owner ImageArtifactOwner, modelID int, requestID, media string, data []byte) (string, error) {
	if owner.UserID <= 0 || modelID <= 0 || len(data) == 0 || len(data) > MaxGatewayImageArtifactBytes {
		return "", errors.New("invalid image artifact")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	id := "img_" + hex.EncodeToString(nonce[:])
	key, _ := imageArtifactKey(id)
	// Existing deployments may share a bucket with public skill assets. Encrypt
	// images with per-object keys held only in the metadata database, so a bucket
	// policy or leaked object path cannot turn into an unauthenticated image URL.
	var encryptionKey [32]byte
	if _, err := rand.Read(encryptionKey[:]); err != nil {
		return "", err
	}
	ciphertext, err := encryptImageArtifact(encryptionKey[:], []byte(id), data)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	// Insert before uploading, so failed/partial writes are discoverable by GC.
	_, err = s.sess.SQL().ExecContext(ctx, `INSERT INTO gateway_image_artifacts
	(id,user_id,instance_id,gateway_key_id,model_id,request_id,media_type,byte_size,encryption_key,ready,created_at,expires_at)
	VALUES (?,?,?,?,?,?,?,?,?,FALSE,?,?)`, id, owner.UserID, owner.InstanceID, owner.GatewayKeyID, modelID, requestID, media, len(data), imageSQLKey{bytes: encryptionKey}, now, now.Add(10*time.Minute))
	if err != nil {
		return "", err
	}
	if err = s.storage.PutObject(ctx, key, ciphertext, "application/octet-stream"); err != nil {
		return "", err
	}
	_, err = s.sess.SQL().ExecContext(ctx, "UPDATE gateway_image_artifacts SET ready=TRUE,expires_at=? WHERE id=?", now.Add(7*24*time.Hour), id)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *gatewayImageArtifactService) Load(ctx context.Context, owner ImageArtifactOwner, id string) (*ImageArtifact, error) {
	key, err := imageArtifactKey(id)
	if err != nil || owner.UserID <= 0 {
		return nil, ErrImageArtifactUnavailable
	}
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT model_id,media_type,byte_size,encryption_key FROM gateway_image_artifacts
	WHERE id=? AND user_id=? AND ready=TRUE AND expires_at>?
	AND (? IS NULL OR instance_id=?) AND (? IS NULL OR gateway_key_id=?)`,
		id, owner.UserID, time.Now().UTC(), owner.InstanceID, owner.InstanceID, owner.GatewayKeyID, owner.GatewayKeyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrImageArtifactUnavailable
	}
	result := &ImageArtifact{ID: id}
	var size int64
	var encryptionKey []byte
	if err = rows.Scan(&result.ModelID, &result.MediaType, &size, &encryptionKey); err != nil {
		return nil, err
	}
	if size <= 0 || size > MaxGatewayImageArtifactBytes {
		return nil, ErrImageArtifactUnavailable
	}
	rows.Close()
	ciphertext, err := s.storage.GetObjectLimited(ctx, key, maxStoredImageArtifactBytes)
	if err != nil {
		return nil, ErrImageArtifactUnavailable
	}
	result.Data, err = decryptImageArtifact(encryptionKey, []byte(id), ciphertext)
	if err != nil || int64(len(result.Data)) != size {
		return nil, ErrImageArtifactUnavailable
	}
	return result, nil
}

// Optional recovery extension. A crash between Save and the job's final update
// must not hide artifacts that were already durably committed. Only ready,
// unexpired artifacts belonging to the authenticated scope are exposed.
func (s *gatewayImageArtifactService) ListForRequest(ctx context.Context, owner ImageArtifactOwner, requestID string) ([]string, error) {
	if owner.UserID <= 0 || requestID == "" || len(requestID) > 64 {
		return nil, ErrImageArtifactUnavailable
	}
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT id FROM gateway_image_artifacts
	WHERE request_id=? AND user_id=? AND ready=TRUE AND expires_at>?
	AND (? IS NULL OR instance_id=?) AND (? IS NULL OR gateway_key_id=?)
	ORDER BY created_at,id LIMIT 10`, requestID, owner.UserID, time.Now().UTC(), owner.InstanceID, owner.InstanceID, owner.GatewayKeyID, owner.GatewayKeyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func encryptImageArtifact(key, associated, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, associated), nil
}

func decryptImageArtifact(key, associated, ciphertext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrImageArtifactUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize()+gcm.Overhead() {
		return nil, ErrImageArtifactUnavailable
	}
	return gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], associated)
}

// Concurrent replicas may delete the same expired object: deletion is idempotent.
// Failed deletes retain metadata and are retried; no directory/glob deletion.
func (s *gatewayImageArtifactService) PurgeExpired(ctx context.Context) error {
	rows, err := s.sess.SQL().QueryContext(ctx, "SELECT id FROM gateway_image_artifacts WHERE expires_at<=? ORDER BY expires_at LIMIT 100", time.Now().UTC())
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		key, err := imageArtifactKey(id)
		if err != nil {
			return err
		}
		if err = s.storage.DeleteObject(ctx, key); err != nil {
			return fmt.Errorf("image cleanup object deletion failed")
		}
		if _, err = s.sess.SQL().ExecContext(ctx, "DELETE FROM gateway_image_artifacts WHERE id=? AND expires_at<=?", id, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (s *gatewayImageArtifactService) RunCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if err := s.PurgeExpired(cleanupCtx); err != nil {
				log.Print("Gateway image artifact cleanup failed; will retry")
			}
			cancel()
		}
	}
}
