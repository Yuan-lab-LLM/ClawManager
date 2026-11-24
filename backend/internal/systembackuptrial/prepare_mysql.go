package systembackuptrial

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"net"
	"regexp"
	"strings"
	"time"

	appdb "clawreef/internal/db"
	domain "clawreef/internal/systembackuptrial/records"

	"github.com/go-sql-driver/mysql"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
)

var preparationDatabaseName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var preparationUUID = regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)
var preparationPassword = regexp.MustCompile(`^Cm9![a-f0-9]{64}$`)

type preparationAccount struct {
	role   string
	secret string
}

func (s *Service) prepareMySQL(ctx context.Context, p trialEnvironment, secrets coreclient.SecretInterface) error {
	owner, err := s.databaseConfig("PREPARE_OWNER")
	database := s.value("PREPARE_DATABASE")
	sourceUUID, targetUUID := s.value("SOURCE_UUID"), s.value("RESTORE_UUID")
	host := s.value("PREPARE_ACCOUNT_HOST")
	if err != nil || owner.DBName != database || !preparationDatabaseName.MatchString(database) ||
		!preparationUUID.MatchString(sourceUUID) || (host != "%" && host != "localhost" && net.ParseIP(host) == nil) {
		return errors.New("trial preparation SQL inputs invalid")
	}
	local := p.local()
	uuid := sourceUUID
	accounts := []preparationAccount{{role: "source", secret: local.SourceReadSecret}}
	if p.Side == "target" {
		if !strings.HasPrefix(database, "cm_drill_trial_") || !preparationUUID.MatchString(targetUUID) || sourceUUID == targetUUID || s.value("RESTORE_ISOLATED") != "true" {
			return errors.New("trial preparation target SQL isolation mismatch")
		}
		uuid = targetUUID
		accounts = []preparationAccount{{role: "restore", secret: local.RestoreWriteSecret}, {role: "read", secret: local.RestoreReadSecret}}
		if local.RuntimeSecret != "" {
			accounts = append(accounts, preparationAccount{role: "runtime", secret: local.RuntimeSecret})
		}
	}
	owner.Logger = &mysql.NopLogger{}
	db, err := openTrialMySQL(owner)
	if err != nil {
		return errors.New("trial preparation SQL owner unavailable")
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.New("trial preparation SQL owner connection failed")
	}
	defer func() {
		// Close the server session and release its named lock, including when
		// the job context was canceled during a SQL operation.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
	}()
	var actualDB, actualUUID string
	if conn.QueryRowContext(ctx, "SELECT DATABASE(), @@server_uuid").Scan(&actualDB, &actualUUID) != nil || actualDB != database || actualUUID != uuid {
		return errors.New("trial preparation SQL identity mismatch")
	}
	var locked bool
	lock := "cm-trial-prep-" + preparationDigest(uuid, database)[:32]
	if conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", lock).Scan(&locked) != nil || !locked {
		return errors.New("trial preparation SQL lock unavailable")
	}
	quotedDB, err := domain.QuotedGrantDatabase(ctx, conn, database)
	if err != nil {
		return errors.New("trial preparation SQL grant scope unavailable")
	}
	if p.Side == "target" {
		if err := appdb.ApplyPinnedMigrations(ctx, db, appdb.MigrationTarget{
			ServerUUID: uuid, Database: database, CatalogSHA256: s.value("PREPARE_MIGRATION_CATALOG_SHA256"), PrepareTrialRuntime: local.RuntimeSecret != "",
		}); err != nil {
			return errors.New("trial preparation target migrations failed")
		}
	}
	for _, account := range accounts {
		if err := prepareTrialAccount(ctx, conn, owner, secrets, local.Namespace, uuid, host, quotedDB, account); err != nil {
			return err
		}
	}
	return nil
}

func preparationDigest(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func prepareTrialAccount(ctx context.Context, conn *sql.Conn, owner *mysql.Config, secrets coreclient.SecretInterface, namespace, uuid, host, quotedDB string, account preparationAccount) error {
	secret, err := ownedTrialSecret(ctx, secrets, namespace, account.secret, "dsn")
	if err != nil {
		return err
	}
	name := "cmtr_" + preparationDigest("trial-mysql-account.v1", uuid, owner.DBName, namespace, account.role)[:25]
	bound := preparationDigest("trial-mysql-secret.v1", name, owner.Net, owner.Addr, owner.TLSConfig, host, account.secret, string(secret.UID))
	child := mysql.NewConfig()
	child.User, child.DBName, child.Net, child.Addr, child.TLSConfig = name, owner.DBName, owner.Net, owner.Addr, owner.TLSConfig
	child.Timeout, child.ReadTimeout, child.WriteTimeout = 5*time.Second, 30*time.Second, 30*time.Second
	child.Logger = &mysql.NopLogger{}
	if len(secret.Data) == 0 {
		if secret.Annotations[trialCredentialBinding] != "" {
			return errors.New("trial preparation cannot replace missing SQL credentials")
		}
		var generated [32]byte
		if _, err := rand.Read(generated[:]); err != nil {
			return errors.New("trial preparation SQL password generation failed")
		}
		child.Passwd = "Cm9!" + hex.EncodeToString(generated[:])
		clear(generated[:])
		secret.Annotations[trialCredentialBinding] = bound
		if err := storeTrialSecret(ctx, secrets, secret, "dsn", []byte(child.FormatDSN())); err != nil {
			return err
		}
	} else {
		saved, err := mysql.ParseDSN(string(secret.Data["dsn"]))
		if err != nil || !preparationPassword.MatchString(saved.Passwd) || secret.Annotations[trialCredentialBinding] != bound {
			return errors.New("trial preparation credential binding mismatch")
		}
		child.Passwd = saved.Passwd
		if child.FormatDSN() != string(secret.Data["dsn"]) {
			return errors.New("trial preparation credential endpoint mismatch")
		}
	}
	var recorded sql.NullString
	err = conn.QueryRowContext(ctx, `SELECT ATTRIBUTE->>'$.clawmanager_trial' FROM information_schema.USER_ATTRIBUTES WHERE USER=? AND HOST=?`, name, host).Scan(&recorded)
	identity := "'" + name + "'@'" + host + "'"
	if errors.Is(err, sql.ErrNoRows) {
		tls := " REQUIRE NONE"
		if child.TLSConfig == "true" {
			tls = " REQUIRE SSL"
		}
		if _, err := conn.ExecContext(ctx, "CREATE USER "+identity+" IDENTIFIED BY '"+child.Passwd+"'"+tls+" ACCOUNT LOCK ATTRIBUTE '{\"clawmanager_trial\":\""+bound+"\"}'"); err != nil {
			return errors.New("trial preparation SQL account creation failed")
		}
	} else if err != nil || !recorded.Valid || recorded.String != bound {
		return errors.New("trial preparation SQL account ownership mismatch")
	}
	grants := []string{"SELECT, INSERT, UPDATE, DELETE ON " + quotedDB + ".*"}
	if account.role == "source" {
		grants = nil
		for _, table := range domain.TrialSourceTables() {
			grants = append(grants, "SELECT ON `"+owner.DBName+"`.`"+table+"`")
		}
	} else if account.role == "read" {
		grants = []string{"SELECT ON " + quotedDB + ".*"}
	}
	for _, grant := range grants {
		if _, err := conn.ExecContext(ctx, "GRANT "+grant+" TO "+identity); err != nil {
			return errors.New("trial preparation SQL grant failed")
		}
	}
	if !verifyPreparedGrants(ctx, conn, identity, grants) {
		return errors.New("trial preparation SQL grants differ")
	}
	if _, err := conn.ExecContext(ctx, "ALTER USER "+identity+" ACCOUNT UNLOCK"); err != nil {
		return errors.New("trial preparation SQL account activation failed")
	}
	reader, err := openTrialMySQL(child)
	if err != nil {
		return errors.New("trial preparation credential verification failed")
	}
	defer reader.Close()
	target := domain.DrillTarget{Database: owner.DBName, ServerUUID: uuid, CheckIsolation: func(ctx context.Context) error { return ctx.Err() }}
	switch account.role {
	case "source":
		err = domain.CheckTrialSource(ctx, reader, owner.DBName, uuid)
	case "read":
		err = domain.CheckTrialTargetReader(ctx, reader, target)
		if err == nil {
			err = domain.CheckTrialRestoreSchema(ctx, reader)
		}
	default:
		err = domain.CheckTrialRuntimeWriter(ctx, reader, target)
	}
	if err != nil {
		return errors.New("trial preparation credential verification failed")
	}
	return nil
}

func verifyPreparedGrants(ctx context.Context, conn *sql.Conn, identity string, expected []string) bool {
	rows, err := conn.QueryContext(ctx, "SHOW GRANTS FOR "+identity)
	if err != nil {
		return false
	}
	defer rows.Close()
	pending := make(map[string]bool, len(expected))
	for _, grant := range expected {
		pending[grant] = true
	}
	for rows.Next() {
		var grant string
		if rows.Scan(&grant) != nil || !strings.HasPrefix(grant, "GRANT ") || strings.Contains(grant, " WITH GRANT OPTION") {
			return false
		}
		scope, _, ok := strings.Cut(strings.TrimPrefix(grant, "GRANT "), " TO ")
		if scope == "USAGE ON *.*" {
			continue
		}
		if !ok || !pending[scope] {
			return false
		}
		delete(pending, scope)
	}
	return rows.Err() == nil && len(pending) == 0
}
