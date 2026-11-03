package repository

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4/adapter/mysql"
)

// Opt in only to a disposable localhost database; never use application config.
func runtimeSecurityMySQL(t *testing.T) *instanceRepository {
	t.Helper()
	portText := os.Getenv("TEST_RUNTIME_SECURITY_MYSQL_PORT")
	if portText == "" {
		t.Skip("set TEST_RUNTIME_SECURITY_MYSQL_PORT for isolated localhost persistence tests")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("invalid localhost MySQL port")
	}
	url := mysql.ConnectionURL{Host: "127.0.0.1:" + portText, User: "root", Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	root, err := mysql.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("runtime_security_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"); err != nil {
		root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.SQL().Exec("DROP DATABASE " + name); _ = root.Close() })
	url.Database = name
	sess, err := mysql.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	// Generic Update writes the complete Instance model. Give its unrelated fields
	// a minimal schema, then apply the real security-profile migration under test.
	var columns []string
	typ := reflect.TypeOf(models.Instance{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		column := strings.Split(field.Tag.Get("db"), ",")[0]
		if column == "" || column == "-" || column == "runtime_security_profile" {
			continue
		}
		columnType := field.Type
		nullable := columnType.Kind() == reflect.Pointer
		if nullable {
			columnType = columnType.Elem()
		}
		sqlType := ""
		switch columnType.Kind() {
		case reflect.Int, reflect.Int64:
			sqlType = "BIGINT"
		case reflect.Float64:
			sqlType = "DOUBLE"
		case reflect.Bool:
			sqlType = "BOOLEAN"
		case reflect.String:
			sqlType = "TEXT"
		default:
			if columnType == reflect.TypeOf(time.Time{}) {
				sqlType = "DATETIME(6)"
			} else {
				t.Fatalf("unhandled test column %s: %s", column, columnType)
			}
		}
		if column == "id" {
			sqlType = "INT PRIMARY KEY"
		} else if !nullable {
			switch columnType.Kind() {
			case reflect.String:
				sqlType += " NOT NULL DEFAULT ''"
			case reflect.Struct:
				sqlType += " NOT NULL DEFAULT CURRENT_TIMESTAMP(6)"
			default:
				sqlType += " NOT NULL DEFAULT 0"
			}
		}
		columns = append(columns, "`"+column+"` "+sqlType)
	}
	if _, err = sess.SQL().Exec("CREATE TABLE instances (" + strings.Join(columns, ",") + ")"); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"077_add_runtime_security_profile.sql", "066_add_runtime_upgrade_fence.sql"} {
		migration, err := os.ReadFile("../db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = sess.SQL().Exec(string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = sess.SQL().Exec("INSERT INTO instances (id,user_id,name,status,runtime_generation) VALUES (7,3,'before','creating',2)"); err != nil {
		t.Fatal(err)
	}
	return &instanceRepository{sess: sess}
}

func TestRuntimeSecurityProtectionSurvivesStaleGenericUpdate(t *testing.T) {
	r := runtimeSecurityMySQL(t)
	ctx := context.Background()
	stale, err := r.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	if stale.RuntimeSecurityProfile != "" {
		t.Fatal("fixture must start legacy")
	}
	if err = r.ProtectRuntimeGeneration(ctx, 7, 2, "isolated-network-v1"); err != nil {
		t.Fatal(err)
	}
	stale.Name = "renamed after protection"
	if err = r.Update(stale); err != nil {
		t.Fatal(err)
	}
	current, err := r.GetByIDContext(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if current.RuntimeSecurityProfile != "isolated-network-v1" || current.Name != stale.Name {
		t.Fatalf("stale update erased protection or lost normal edit: profile=%q name=%q", current.RuntimeSecurityProfile, current.Name)
	}
	// Generic writes must also omit nonempty marker values; only the dedicated
	// conditional transition may change this authoritative field.
	stale.RuntimeSecurityProfile = "future-profile"
	if err = r.Update(stale); err != nil {
		t.Fatal(err)
	}
	current, err = r.GetByIDContext(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if current.RuntimeSecurityProfile != "isolated-network-v1" {
		t.Fatalf("generic update changed authoritative profile: %q", current.RuntimeSecurityProfile)
	}
}

func TestRuntimeSecurityProtectionRejectsUnknownStoredProfiles(t *testing.T) {
	r := runtimeSecurityMySQL(t)
	ctx := context.Background()
	for _, profile := range []string{"future-profile", " ", "\t", "ISOLATED-NETWORK-V1", "isolated-network-v1 "} {
		t.Run(fmt.Sprintf("%q", profile), func(t *testing.T) {
			if _, err := r.sess.SQL().Exec("UPDATE instances SET runtime_security_profile = ? WHERE id = 7", profile); err != nil {
				t.Fatal(err)
			}
			if err := r.ProtectRuntimeGeneration(ctx, 7, 2, "isolated-network-v1"); err == nil {
				t.Errorf("unknown profile %q was replaced", profile)
			}
			current, err := r.GetByIDContext(ctx, 7)
			if err != nil {
				t.Fatal(err)
			}
			if current.RuntimeSecurityProfile != profile {
				t.Errorf("unknown marker changed from %q to %q", profile, current.RuntimeSecurityProfile)
			}
		})
	}
}

func TestRuntimeSecurityProtectionRejectsMissingInstance(t *testing.T) {
	r := runtimeSecurityMySQL(t)
	err := r.ProtectRuntimeGeneration(context.Background(), 8, 2, "isolated-network-v1")
	if err == nil || !strings.Contains(err.Error(), "runtime security transition lost assignment") {
		t.Fatalf("missing instance did not fail the security transition: %v", err)
	}
}
