package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/historymaintenance"
	"clawreef/internal/repository"
	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
)

func TestWholeBranchMonthlyExportContinuesLargeInlinePages(t *testing.T) {
	port := os.Getenv("TEST_DATABASE_MYSQL_PORT")
	if port == "" {
		t.Skip("explicit isolated fixture required")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		t.Fatal("invalid fixture port")
	}
	root, err := sql.Open("mysql", fmt.Sprintf("root:%s@tcp(127.0.0.1:%d)/mysql?parseTime=true", os.Getenv("TEST_DATABASE_MYSQL_PASSWORD"), n))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var marker int
	if err = root.QueryRow(`SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='cm_database_test_marker'`).Scan(&marker); err != nil || marker != 1 {
		t.Fatal("isolated fixture marker required", err)
	}
	name := fmt.Sprintf("cm_database_test_%d", time.Now().UnixNano())
	if _, err = root.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	defer root.Exec("DROP DATABASE " + name)
	s, err := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: n, User: "root", Password: os.Getenv("TEST_DATABASE_MYSQL_PASSWORD"), Database: name, MaxOpenConns: 4, MaxIdleConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pool := s.Driver().(*sql.DB)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Second)
	history := historymaintenance.New(pool, nil, nil)
	if err = history.PrepareMonths(ctx, at); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 20; id++ {
		if _, err = pool.Exec(`INSERT INTO model_invocations(id,user_id,trace_id,request_id,provider_type,requested_model,actual_provider_model,traffic_class,status,request_payload,created_at)VALUES(?,1,?,?,'openai','m','m','llm','completed',?,?)`, id, fmt.Sprint(id), fmt.Sprint(id), strings.Repeat("B", 2<<20), at); err != nil {
			t.Fatal(err)
		}
		if _, err = repository.ReconcileObservationSummaries(ctx, s, 1); err != nil {
			t.Fatal(err)
		}
	}
	month, _ := historymaintenance.MonthlyTable("model_invocations", at)
	if _, complete, e := history.VerifyLogMonth(ctx, month, 0); e != nil || !complete {
		t.Fatalf("verify %v %v", complete, e)
	}
	if err = history.CutoverLogMonth(ctx, month); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", 1); c.Next() })
	router.GET("/history/:kind/export", NewHistoryArchiveHandler(history, false).Export)
	cursor := int64(0)
	seen := 0
	pages := 0
	for {
		url := fmt.Sprintf("/history/model_invocations/export?limit=200&since=%s&until=%s&after_id=%d", at.Format(time.RFC3339), at.Add(time.Hour).Format(time.RFC3339), cursor)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		if w.Code != 200 {
			t.Fatalf("export HTTP%d", w.Code)
		}
		decoder := json.NewDecoder(w.Body)
		var bytes int64
		count := 0
		for {
			var row historymaintenance.Record
			err = decoder.Decode(&row)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			seen++
			count++
			if row.ID != int64(seen) {
				t.Fatalf("export skipped/duplicated ID %d want%d", row.ID, seen)
			}
			bytes += int64(len(row.Payload))
		}
		if count == 0 || count > 3 || bytes > historymaintenance.MaxPageBytes {
			t.Fatalf("export page count=%d bytes=%d", count, bytes)
		}
		cursor, err = strconv.ParseInt(w.Header().Get("X-History-Next-Cursor"), 10, 64)
		if err != nil || cursor != int64(seen) {
			t.Fatalf("export cursor %d %v", cursor, err)
		}
		pages++
		if w.Header().Get("X-History-Has-More") == "false" {
			break
		}
		if pages > 20 {
			t.Fatal("export continuation loop")
		}
	}
	if seen != 20 || pages != 7 {
		t.Fatalf("export rows=%d pages=%d", seen, pages)
	}
	t.Logf("actual HTTP NDJSON export: %d complete 2MiB legacy rows across %d byte-bounded pages", seen, pages)
}
