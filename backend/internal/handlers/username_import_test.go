package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

func TestLDAPPreviewRejectsOccupiedUsernameAndAcceptsExistingIdentity(t *testing.T) {
	svc := newFakeLDAPImportUserService()
	svc.existingByExternalID["local"] = &models.User{ID: 1, Username: "alice", Email: "alice@example.com", AuthProvider: "local"}
	svc.existingByExternalID["uid=bob,ou=People"] = &models.User{ID: 2, Username: "bob", AuthProvider: "ldap", ExternalID: stringPtrValueForTest("uid=bob,ou=People")}
	directory := &fakeLDAPImportDirectory{users: []services.LDAPDirectoryUser{
		{Username: "ALICE", ExternalID: "uid=alice,ou=People", Email: "alice@example.com"},
		{Username: "bob", ExternalID: "uid=bob,ou=People"},
		{Username: "carol", ExternalID: "uid=carol,ou=People"},
	}}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/users/import/ldap/preview", nil)
	NewUserHandler(svc, &fakeLDAPImportQuotaService{}, directory).PreviewLDAPUsers(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview: %s", recorder.Body.String())
	}
	var result struct {
		Data struct{ Users []LDAPImportUser }
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	users := result.Data.Users
	if len(users) != 3 || users[0].Status != "invalid" || users[0].Error != "username already exists" || users[1].Status != "exists" || users[2].Status != "ready" {
		t.Fatalf("unexpected preview: %#v", users)
	}
}

func TestLDAPImportFailsOnlyConflictingRows(t *testing.T) {
	svc := newFakeLDAPImportUserService()
	svc.existingByExternalID["local"] = &models.User{ID: 7, Username: "alice", Email: "alice@example.com", AuthProvider: "local"}
	svc.existingByExternalID["uid=old,ou=People"] = &models.User{ID: 8, Username: "old", AuthProvider: "ldap", ExternalID: stringPtrValueForTest("uid=old,ou=People")}
	directory := &fakeLDAPImportDirectory{users: []services.LDAPDirectoryUser{
		{Username: "ALICE", ExternalID: "uid=alice,ou=People", Email: "alice@example.com"},
		{Username: "bob", ExternalID: "uid=bob,ou=One", Email: "bob@example.com"},
		{Username: "BOB", ExternalID: "uid=bob,ou=Two", Email: "bob@example.com"},
		{Username: "old", ExternalID: "uid=old,ou=People"},
		{Username: "carol", ExternalID: "uid=carol,ou=People"},
	}}
	result := performLDAPImport(t, NewUserHandler(svc, &fakeLDAPImportQuotaService{}, directory), LDAPImportRequest{Role: "user"})
	if result.Data.CreatedCount != 2 || result.Data.FailedCount != 2 || result.Data.SkippedCount != 1 || result.Data.UpdatedCount != 0 {
		t.Fatalf("unexpected import counts: %#v", result.Data)
	}
	for _, failure := range result.Data.Errors {
		if failure.Error != "username already exists" {
			t.Fatalf("unexpected failure: %#v", failure)
		}
	}
	if svc.createdByExternalID["uid=bob,ou=One"] == nil || svc.createdByExternalID["uid=bob,ou=Two"] != nil {
		t.Fatal("first successful row must claim username")
	}
	if svc.existingByExternalID["uid=old,ou=People"].LoginAlias != nil {
		t.Fatal("re-import must not backfill aliases")
	}
}

func TestCSVImportRejectsCrossProviderUsernameConflicts(t *testing.T) {
	svc := newFakeLDAPImportUserService()
	svc.existingByExternalID["local"] = &models.User{ID: 7, Username: "alice", AuthProvider: "local"}
	svc.existingByExternalID["uid=bob,ou=People"] = &models.User{ID: 8, Username: "bob", AuthProvider: "ldap"}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "users.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write([]byte("Username,Role,Auth Provider,External ID,Max Instances,Max CPU Cores,Max Memory GB,Max Storage GB\n" +
		"alice,user,ldap,uid=alice,1,1,1,1\n" +
		"bob,user,local,,1,1,1,1\n" +
		"carol,user,ldap,uid=carol,1,1,1,1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/users/import", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	NewUserHandler(svc, &fakeLDAPImportQuotaService{}).ImportUsers(c)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("CSV import: %s", recorder.Body.String())
	}
	var result ldapImportTestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.CreatedCount != 1 || result.Data.FailedCount != 2 {
		t.Fatalf("CSV result: %#v", result.Data)
	}
	for _, failure := range result.Data.Errors {
		if failure.Error != "username already exists" {
			t.Fatalf("CSV failure: %#v", failure)
		}
	}
}
