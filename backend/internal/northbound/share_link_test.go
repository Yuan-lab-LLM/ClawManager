package northbound

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type northboundInstanceStub struct {
	items        map[int]*models.Instance
	restartCalls []int
	resetCalls   []int
	deleteCalls  []int
}

func (s *northboundInstanceStub) Restart(id int) error {
	s.restartCalls = append(s.restartCalls, id)
	return nil
}

func (s *northboundInstanceStub) Reset(id int) error {
	s.resetCalls = append(s.resetCalls, id)
	return nil
}

func (s *northboundInstanceStub) DeletePermanently(id int) error {
	s.deleteCalls = append(s.deleteCalls, id)
	delete(s.items, id)
	return nil
}

func (s *northboundInstanceStub) Create(int, services.CreateInstanceRequest) (*models.Instance, error) {
	return nil, errors.New("not implemented")
}

func (s *northboundInstanceStub) GetByID(id int) (*models.Instance, error) {
	return s.items[id], nil
}

func (s *northboundInstanceStub) GetByUserID(userID, offset, limit int) ([]models.Instance, int, error) {
	items := make([]models.Instance, 0)
	for _, item := range s.items {
		if item.UserID == userID {
			items = append(items, *item)
		}
	}
	return items, len(items), nil
}

func (s *northboundInstanceStub) GetLiteByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error) {
	items := make([]models.Instance, 0)
	for _, item := range s.items {
		if item.UserID == userID && item.Owner != nil && *item.Owner == owner && isLite(item) {
			items = append(items, *item)
		}
	}
	total := len(items)
	if offset >= total {
		return []models.Instance{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return items[offset:end], total, nil
}

func (s *northboundInstanceStub) GetWorkbuddyProByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error) {
	items := make([]models.Instance, 0)
	for _, item := range s.items {
		if item.UserID == userID && item.Owner != nil && *item.Owner == owner && isWorkbuddyLinuxPro(item) {
			items = append(items, *item)
		}
	}
	total := len(items)
	if offset >= total {
		return []models.Instance{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return items[offset:end], total, nil
}

func (s *northboundInstanceStub) GetProByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error) {
	items := make([]models.Instance, 0)
	for _, item := range s.items {
		if item.UserID == userID && item.Owner != nil && *item.Owner == owner && isSupportedNorthboundProInstance(item) {
			items = append(items, *item)
		}
	}
	total := len(items)
	if offset >= total {
		return []models.Instance{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return items[offset:end], total, nil
}

func (s *northboundInstanceStub) GetNorthboundByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error) {
	items := make([]models.Instance, 0)
	for _, item := range s.items {
		if item.UserID == userID && item.Owner != nil && *item.Owner == owner && isSupportedNorthboundInstance(item) {
			items = append(items, *item)
		}
	}
	total := len(items)
	if offset >= total {
		return []models.Instance{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return items[offset:end], total, nil
}

type shareLinkResetStub struct {
	passwordCreateResult *services.PasswordExternalAccessResult
	urlResult            *services.EnableShareLinkResult
	passwordResult       *services.PasswordExternalAccessResult
	passwordCreateErr    error
	urlErr               error
	passwordErr          error
	passwordCreateCalls  int
	urlCalls             int
	passwordCalls        int
	lastInstanceID       int
	lastCreatedBy        int
	lastExpiration       services.ExternalAccessExpirationRequest
}

func (s *shareLinkResetStub) CreatePassword(_ context.Context, instanceID, createdBy int, expiration services.ExternalAccessExpirationRequest) (*services.PasswordExternalAccessResult, error) {
	s.passwordCreateCalls++
	s.lastInstanceID = instanceID
	s.lastCreatedBy = createdBy
	s.lastExpiration = expiration
	return s.passwordCreateResult, s.passwordCreateErr
}

func (s *shareLinkResetStub) ResetURL(_ context.Context, instanceID, createdBy int) (*services.EnableShareLinkResult, error) {
	s.urlCalls++
	s.lastInstanceID = instanceID
	s.lastCreatedBy = createdBy
	return s.urlResult, s.urlErr
}

func (s *shareLinkResetStub) ResetPassword(_ context.Context, instanceID, createdBy int) (*services.PasswordExternalAccessResult, error) {
	s.passwordCalls++
	s.lastInstanceID = instanceID
	s.lastCreatedBy = createdBy
	return s.passwordResult, s.passwordErr
}

func TestCoreServiceResetsOwnedLiteShareLink(t *testing.T) {
	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour)
	access := &models.InstanceExternalAccess{
		InstanceID:      12,
		Enabled:         true,
		AuthMode:        services.ExternalAccessModePassword,
		WorkspaceAccess: services.ExternalWorkspaceAccessRead,
		ExpiresAt:       &expiresAt,
		UpdatedAt:       now,
	}
	resetter := &shareLinkResetStub{
		urlResult: &services.EnableShareLinkResult{
			Access: access, ShareURL: "/s/sl_new/",
		},
		passwordResult: &services.PasswordExternalAccessResult{
			Access: access, ShareURL: "/s/sl_new/", Password: "pwd_new",
		},
	}
	service := &CoreService{
		instances: &northboundInstanceStub{items: map[int]*models.Instance{
			12: {ID: 12, UserID: 7, Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModeLite},
		}},
		externalAccess: resetter,
	}
	principal := Principal{UserID: 7, SessionID: "nbs_test", Scopes: []string{ScopeShareLinkReset}}

	urlResult, err := service.ResetShareLinkURL(context.Background(), principal, 12)
	if err != nil {
		t.Fatalf("ResetShareLinkURL failed: %v", err)
	}
	if urlResult.ShareURL != "/s/sl_new/" || urlResult.Password != "" || urlResult.WorkspaceAccess != services.ExternalWorkspaceAccessRead {
		t.Fatalf("unexpected URL reset response: %+v", urlResult)
	}
	passwordResult, err := service.ResetShareLinkPassword(context.Background(), principal, 12)
	if err != nil {
		t.Fatalf("ResetShareLinkPassword failed: %v", err)
	}
	if passwordResult.ShareURL != "/s/sl_new/" || passwordResult.Password != "pwd_new" {
		t.Fatalf("unexpected password reset response: %+v", passwordResult)
	}
	if resetter.urlCalls != 1 || resetter.passwordCalls != 1 || resetter.lastInstanceID != 12 || resetter.lastCreatedBy != 7 {
		t.Fatalf("unexpected reset calls: %+v", resetter)
	}
}

func TestCoreServiceEnablesPasswordShareLinkForOwnedLiteInstance(t *testing.T) {
	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour)
	access := &models.InstanceExternalAccess{
		InstanceID:      12,
		Enabled:         true,
		AuthMode:        services.ExternalAccessModePassword,
		WorkspaceAccess: services.ExternalWorkspaceAccessNone,
		ExpiresAt:       &expiresAt,
		UpdatedAt:       now,
	}
	externalAccess := &shareLinkResetStub{passwordCreateResult: &services.PasswordExternalAccessResult{
		Access: access, ShareURL: "/s/sl_created/", Password: "pwd_created",
	}}
	service := &CoreService{
		instances: &northboundInstanceStub{items: map[int]*models.Instance{
			12: {ID: 12, UserID: 7, Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModeLite},
		}},
		externalAccess: externalAccess,
	}
	principal := Principal{UserID: 7, SessionID: "nbs_test", Scopes: []string{ScopeShareLinkManage}}

	result, err := service.EnableShareLinkPassword(context.Background(), principal, 12, EnableShareLinkPasswordRequest{})
	if err != nil {
		t.Fatalf("EnableShareLinkPassword failed: %v", err)
	}
	if result.ShareURL != "/s/sl_created/" || result.Password != "pwd_created" || result.AuthMode != services.ExternalAccessModePassword {
		t.Fatalf("unexpected password enable response: %+v", result)
	}
	if externalAccess.passwordCreateCalls != 1 || externalAccess.lastInstanceID != 12 || externalAccess.lastCreatedBy != 7 {
		t.Fatalf("unexpected password enable calls: %+v", externalAccess)
	}
	if externalAccess.lastExpiration.Mode != services.ExternalAccessExpirationPreset ||
		externalAccess.lastExpiration.Preset != services.ExternalAccessPreset24Hours ||
		externalAccess.lastExpiration.WorkspaceAccess != services.ExternalWorkspaceAccessNone {
		t.Fatalf("unexpected default expiration: %+v", externalAccess.lastExpiration)
	}
}

func TestCoreServiceEnablesPasswordShareLinkForOwnedLinuxWorkbuddyPro(t *testing.T) {
	now := time.Now().UTC()
	externalAccess := &shareLinkResetStub{passwordCreateResult: &services.PasswordExternalAccessResult{
		Access: &models.InstanceExternalAccess{
			InstanceID: 21, Enabled: true, AuthMode: services.ExternalAccessModePassword,
			WorkspaceAccess: services.ExternalWorkspaceAccessWrite, UpdatedAt: now,
		},
		ShareURL: "/s/sl_workbuddy/", Password: "pwd_workbuddy",
	}}
	service := &CoreService{
		instances: &northboundInstanceStub{items: map[int]*models.Instance{
			21: {
				ID: 21, UserID: 7, Type: "workbuddy", RuntimeVariant: services.WorkbuddyRuntimeLinux,
				InstanceMode: services.InstanceModePro, RuntimeType: services.RuntimeBackendDesktop,
			},
		}},
		externalAccess: externalAccess,
	}
	principal := Principal{UserID: 7, SessionID: "nbs_test", Scopes: []string{ScopeShareLinkManage}}

	result, err := service.EnableShareLinkPassword(context.Background(), principal, 21, EnableShareLinkPasswordRequest{
		WorkspaceAccess: services.ExternalWorkspaceAccessWrite,
	})
	if err != nil {
		t.Fatalf("EnableShareLinkPassword for WorkBuddy failed: %v", err)
	}
	if result.InstanceID != 21 || result.ShareURL != "/s/sl_workbuddy/" || result.Password != "pwd_workbuddy" ||
		result.WorkspaceAccess != services.ExternalWorkspaceAccessWrite {
		t.Fatalf("unexpected WorkBuddy password enable response: %+v", result)
	}
	if externalAccess.passwordCreateCalls != 1 || externalAccess.lastInstanceID != 21 ||
		externalAccess.lastExpiration.WorkspaceAccess != services.ExternalWorkspaceAccessWrite {
		t.Fatalf("unexpected WorkBuddy external access call: %+v", externalAccess)
	}
}

func TestCoreServiceRejectsForeignOrInvalidPasswordShareLinkEnable(t *testing.T) {
	externalAccess := &shareLinkResetStub{}
	service := &CoreService{
		instances: &northboundInstanceStub{items: map[int]*models.Instance{
			12: {ID: 12, UserID: 7, Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModeLite},
		}},
		externalAccess: externalAccess,
	}
	foreign := Principal{UserID: 8, SessionID: "nbs_other", Scopes: []string{ScopeShareLinkManage}}
	if _, err := service.EnableShareLinkPassword(context.Background(), foreign, 12, EnableShareLinkPasswordRequest{}); apiErrorCode(err) != "INSTANCE_NOT_FOUND" {
		t.Fatalf("foreign enable error = %v, want INSTANCE_NOT_FOUND", err)
	}
	if externalAccess.passwordCreateCalls != 0 {
		t.Fatal("foreign instance must be rejected before enabling external access")
	}

	owner := Principal{UserID: 7, SessionID: "nbs_owner", Scopes: []string{ScopeShareLinkManage}}
	past := time.Now().UTC().Add(-time.Minute)
	if _, err := service.EnableShareLinkPassword(context.Background(), owner, 12, EnableShareLinkPasswordRequest{
		ExpiresMode: services.ExternalAccessExpirationCustom,
		ExpiresAt:   &past,
	}); apiErrorCode(err) != "VALIDATION_ERROR" {
		t.Fatalf("past custom expiry error = %v, want VALIDATION_ERROR", err)
	}
	if externalAccess.passwordCreateCalls != 0 {
		t.Fatal("invalid request must be rejected before enabling external access")
	}
}

func TestEnableShareLinkPasswordResponseIsNotCacheable(t *testing.T) {
	now := time.Now().UTC()
	externalAccess := &shareLinkResetStub{passwordCreateResult: &services.PasswordExternalAccessResult{
		Access: &models.InstanceExternalAccess{
			InstanceID: 12, Enabled: true, AuthMode: services.ExternalAccessModePassword,
			WorkspaceAccess: services.ExternalWorkspaceAccessNone, UpdatedAt: now,
		},
		ShareURL: "/s/sl_created/", Password: "pwd_created",
	}}
	service := &CoreService{
		instances: &northboundInstanceStub{items: map[int]*models.Instance{
			12: {ID: 12, UserID: 7, Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModeLite},
		}},
		externalAccess: externalAccess,
	}
	handler := NewCoreHandler(service, "unused-internal-secret")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/lite-instances/:id/external-access/password", func(c *gin.Context) {
		c.Set("northboundPrincipal", &Principal{
			UserID: 7, SessionID: "nbs_test", Scopes: []string{ScopeShareLinkManage},
		})
		handler.EnableShareLinkPassword(c)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/lite-instances/12/external-access/password",
		strings.NewReader(`{"expires_mode":"preset","expires_preset":"1h"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body = %s", response.Code, response.Body.String())
	}
	if value := response.Header().Get("Cache-Control"); value != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", value)
	}
}

func TestCoreServiceHidesForeignShareLinkAndMapsStateConflicts(t *testing.T) {
	resetter := &shareLinkResetStub{urlErr: services.ErrExternalAccessNotEnabled}
	service := &CoreService{
		instances: &northboundInstanceStub{items: map[int]*models.Instance{
			12: {ID: 12, UserID: 7, Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModeLite},
		}},
		externalAccess: resetter,
	}
	principal := Principal{UserID: 8, SessionID: "nbs_other", Scopes: []string{ScopeShareLinkReset}}
	if _, err := service.ResetShareLinkURL(context.Background(), principal, 12); apiErrorCode(err) != "INSTANCE_NOT_FOUND" {
		t.Fatalf("foreign reset error = %v, want INSTANCE_NOT_FOUND", err)
	}
	if resetter.urlCalls != 0 {
		t.Fatal("foreign instance must be rejected before external access mutation")
	}

	principal.UserID = 7
	if _, err := service.ResetShareLinkURL(context.Background(), principal, 12); apiErrorCode(err) != "SHARE_LINK_NOT_ENABLED" {
		t.Fatalf("disabled share link error = %v, want SHARE_LINK_NOT_ENABLED", err)
	}
}

func TestNorthboundResourceRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	publicRouter := gin.New()
	RegisterGatewayRoutes(publicRouter, &AuthHandler{}, &CoreClient{})
	internalRouter := gin.New()
	RegisterCoreRoutes(internalRouter, NewCoreHandler(&CoreService{}, "internal-secret"))

	want := map[string]bool{
		"POST /api/northbound/v1/pro-instances":                                            false,
		"GET /api/northbound/v1/pro-instances":                                             false,
		"GET /api/northbound/v1/pro-instances/:id":                                         false,
		"POST /internal/northbound/v1/pro-instances":                                       false,
		"GET /internal/northbound/v1/pro-instances":                                        false,
		"GET /internal/northbound/v1/pro-instances/:id":                                    false,
		"POST /api/northbound/v1/lite-instances/:id/restart":                               false,
		"POST /api/northbound/v1/lite-instances/:id/reset":                                 false,
		"POST /api/northbound/v1/lite-instances/:id/delete":                                false,
		"POST /api/northbound/v1/pro-instances/:id/restart":                                false,
		"POST /api/northbound/v1/pro-instances/:id/reset":                                  false,
		"POST /api/northbound/v1/pro-instances/:id/delete":                                 false,
		"POST /internal/northbound/v1/lite-instances/:id/restart":                          false,
		"POST /internal/northbound/v1/lite-instances/:id/reset":                            false,
		"POST /internal/northbound/v1/lite-instances/:id/delete":                           false,
		"POST /internal/northbound/v1/pro-instances/:id/restart":                           false,
		"POST /internal/northbound/v1/pro-instances/:id/reset":                             false,
		"POST /internal/northbound/v1/pro-instances/:id/delete":                            false,
		"POST /api/northbound/v1/lite-instances/:id/external-access/password":              false,
		"POST /api/northbound/v1/lite-instances/:id/external-access/share-link/reset":      false,
		"POST /api/northbound/v1/lite-instances/:id/external-access/password/reset":        false,
		"POST /internal/northbound/v1/lite-instances/:id/external-access/password":         false,
		"POST /internal/northbound/v1/lite-instances/:id/external-access/share-link/reset": false,
		"POST /internal/northbound/v1/lite-instances/:id/external-access/password/reset":   false,
	}
	for _, route := range append(publicRouter.Routes(), internalRouter.Routes()...) {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Fatalf("route not registered: %s", route)
		}
	}
}

func apiErrorCode(err error) string {
	var target *APIError
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}
