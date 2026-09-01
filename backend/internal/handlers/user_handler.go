package handlers

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// UserHandler handles user management requests
type UserHandler struct {
	userService      services.UserService
	quotaService     services.QuotaService
	ldapDirectory    services.LDAPDirectory
	enterprisePolicy services.EnterpriseAuthPolicyProvider
}

// NewUserHandler creates a new user handler
func NewUserHandler(userService services.UserService, quotaService services.QuotaService, directories ...services.LDAPDirectory) *UserHandler {
	var ldapDirectory services.LDAPDirectory
	var enterprisePolicy services.EnterpriseAuthPolicyProvider
	if len(directories) > 0 {
		ldapDirectory = directories[0]
		enterprisePolicy, _ = ldapDirectory.(services.EnterpriseAuthPolicyProvider)
	}
	return &UserHandler{
		userService:      userService,
		quotaService:     quotaService,
		ldapDirectory:    ldapDirectory,
		enterprisePolicy: enterprisePolicy,
	}
}

type LDAPImportRequest struct {
	Role         string  `json:"role" binding:"required,oneof=admin user"`
	MaxInstances int     `json:"max_instances" binding:"min=0"`
	MaxCPUCores  float64 `json:"max_cpu_cores" binding:"min=0"`
	MaxMemoryGB  int     `json:"max_memory_gb" binding:"min=0"`
	MaxStorageGB int     `json:"max_storage_gb" binding:"min=0"`
	MaxGPUCount  int     `json:"max_gpu_count" binding:"min=0"`
	Query        string   `json:"query"`
	Limit        int      `json:"limit" binding:"min=0"`
	ExternalIDs  []string `json:"external_ids"`
}

type LDAPImportUser struct {
	services.LDAPDirectoryUser
	Status string `json:"status"`
}

func (h *UserHandler) PreviewLDAPUsers(c *gin.Context) {
	if h.ldapDirectory == nil {
		utils.Error(c, http.StatusServiceUnavailable, "LDAP import is unavailable")
		return
	}
	users, err := h.ldapDirectory.ListUsers(c.Request.Context(), ldapListOptionsFromRequest(c.Query("query"), c.Query("limit")))
	if err != nil {
		utils.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	preview := make([]LDAPImportUser, 0, len(users))
	for _, item := range users {
		if item.Email == "" && item.Username != "" {
			item.Email = ldapImportEmail(item.Username, item.ExternalID)
		}
		status := "ready"
		if item.Error != "" || strings.TrimSpace(item.ExternalID) == "" {
			status = "invalid"
			if item.Error == "" { item.Error = "LDAP DN is required" }
		} else if existing, lookupErr := h.userService.GetUserByExternalIdentity(services.AuthProviderLDAP, item.ExternalID); lookupErr != nil && !isUserNotFound(lookupErr) {
			status = "invalid"
			item.Error = lookupErr.Error()
		} else if existing != nil {
			if existing.LoginAlias == nil || strings.TrimSpace(stringPtrValue(existing.LoginAlias)) == "" {
				status = "pending_alias"
			} else {
				status = "exists"
			}
		} else if item.Email != "" {
			if existing, lookupErr := h.userService.GetUserByEmail(item.Email); lookupErr != nil && !isUserNotFound(lookupErr) {
				status = "invalid"
				item.Error = lookupErr.Error()
			} else if existing != nil {
				status = "exists"
			}
		}
		preview = append(preview, LDAPImportUser{LDAPDirectoryUser: item, Status: status})
	}
	utils.Success(c, http.StatusOK, "LDAP users retrieved successfully", gin.H{"users": preview, "total": len(preview)})
}

func (h *UserHandler) ImportLDAPUsers(c *gin.Context) {
	if h.ldapDirectory == nil {
		utils.Error(c, http.StatusServiceUnavailable, "LDAP import is unavailable")
		return
	}
	var req LDAPImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	selectedExternalIDs := ldapExternalIDSet(req.ExternalIDs)
	users, err := h.ldapDirectory.ListUsers(c.Request.Context(), ldapImportListOptions(req, selectedExternalIDs))
	if err != nil {
		utils.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	created := make([]importedUserCredential, 0)
	updated := make([]updatedLDAPUser, 0)
	skipped := make([]importUserResult, 0)
	failed := make([]importUserResult, 0)
	syncRole := h.syncLDAPImportRoles()
	for index, item := range users {
		line := index + 1
		if len(selectedExternalIDs) > 0 {
			if _, ok := selectedExternalIDs[strings.TrimSpace(item.ExternalID)]; !ok {
				continue
			}
		}
		if item.Error != "" || strings.TrimSpace(item.Username) == "" || strings.TrimSpace(item.ExternalID) == "" {
			errorMessage := item.Error
			if strings.TrimSpace(item.Username) == "" { errorMessage = "LDAP username is required" }
			if strings.TrimSpace(item.ExternalID) == "" { errorMessage = "LDAP DN is required" }
			failed = append(failed, importUserResult{Line: line, Username: item.Username, Error: firstNonEmpty(errorMessage, "LDAP username is required")})
			continue
		}
		email := strings.TrimSpace(item.Email)
		if email == "" {
			email = ldapImportEmail(item.Username, item.ExternalID)
		}
		role := ldapImportRole(req.Role, item.Role, syncRole)
		existing, lookupErr := h.userService.GetUserByExternalIdentity(services.AuthProviderLDAP, item.ExternalID)
		if lookupErr != nil && !isUserNotFound(lookupErr) {
			failed = append(failed, importUserResult{Line: line, Username: item.Username, Error: lookupErr.Error()})
			continue
		}
		if existing != nil {
			if existing.LoginAlias == nil || strings.TrimSpace(stringPtrValue(existing.LoginAlias)) == "" {
				ensured, ensureErr := h.userService.EnsureLDAPLoginAlias(item.ExternalID)
				if ensureErr != nil {
					failed = append(failed, importUserResult{Line: line, Username: item.Username, Error: ensureErr.Error()})
					continue
				}
				existing = ensured
			}
			if syncRole && (existing.Role != role || strings.EqualFold(role, "admin")) {
				if updateErr := h.userService.UpdateUserRole(existing.ID, role); updateErr != nil {
					failed = append(failed, importUserResult{Line: line, Username: item.Username, Error: updateErr.Error()})
					continue
				}
				existing.Role = role
				updated = append(updated, updatedLDAPUser{Username: existing.Username, LoginAlias: stringPtrValue(existing.LoginAlias), Email: existing.Email, Role: existing.Role, AuthProvider: existing.AuthProvider})
				continue
			}
			skipped = append(skipped, importUserResult{Line: line, Username: item.Username, Error: "user already exists"})
			continue
		}
		existing, lookupErr = h.userService.GetUserByEmail(email)
		if lookupErr != nil && !isUserNotFound(lookupErr) {
			failed = append(failed, importUserResult{Line: line, Username: item.Username, Error: lookupErr.Error()})
			continue
		}
		if existing != nil {
			skipped = append(skipped, importUserResult{Line: line, Username: item.Username, Error: "email already exists"})
			continue
		}
		user, createErr := h.userService.CreateUserWithProviderAndExternalID(item.Username, email, "", role, services.AuthProviderLDAP, item.ExternalID)
		if createErr != nil {
			failed = append(failed, importUserResult{Line: line, Username: item.Username, Error: createErr.Error()})
			continue
		}
		importQuota := quotaForImportRole(role, models.UserQuota{MaxInstances: req.MaxInstances, MaxCPUCores: req.MaxCPUCores, MaxMemoryGB: req.MaxMemoryGB, MaxStorageGB: req.MaxStorageGB, MaxGPUCount: req.MaxGPUCount}, syncRole)
		quotaErr := h.quotaService.UpdateUserQuota(user.ID, &importQuota)
		if quotaErr != nil {
			failed = append(failed, importUserResult{Line: line, Username: item.Username, Error: quotaErr.Error()})
			continue
		}
		created = append(created, importedUserCredential{Username: user.Username, LoginAlias: stringPtrValue(user.LoginAlias), Email: user.Email, Role: user.Role, AuthProvider: user.AuthProvider, MaxInstances: importQuota.MaxInstances, MaxCPUCores: importQuota.MaxCPUCores, MaxMemoryGB: importQuota.MaxMemoryGB, MaxStorageGB: importQuota.MaxStorageGB, MaxGPUCount: importQuota.MaxGPUCount})
	}
	utils.Success(c, http.StatusCreated, "LDAP users imported successfully", gin.H{"created_count": len(created), "updated_count": len(updated), "skipped_count": len(skipped), "failed_count": len(failed), "created_users": created, "updated_users": updated, "skipped": skipped, "errors": failed})
}

func ldapListOptionsFromRequest(query, limitValue string) services.LDAPListOptions {
	limit := 0
	if strings.TrimSpace(limitValue) != "" {
		if parsed, err := strconv.Atoi(limitValue); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	return services.LDAPListOptions{Query: strings.TrimSpace(query), Limit: limit}
}

func ldapImportListOptions(req LDAPImportRequest, selected map[string]struct{}) services.LDAPListOptions {
	options := services.LDAPListOptions{
		Query: strings.TrimSpace(req.Query),
		Limit: req.Limit,
	}
	if len(selected) > 0 {
		options.Query = ""
		options.Limit = 0
	}
	return options
}

func ldapExternalIDSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result[trimmed] = struct{}{}
		}
	}
	return result
}

func (h *UserHandler) syncLDAPImportRoles() bool {
	if h.enterprisePolicy == nil {
		return false
	}
	return h.enterprisePolicy.EnterpriseAuthPolicy().SyncRole
}

func ldapImportRole(requestRole, directoryRole string, syncRole bool) string {
	if syncRole {
		if strings.EqualFold(strings.TrimSpace(directoryRole), "admin") {
			return "admin"
		}
		return "user"
	}
	if strings.EqualFold(strings.TrimSpace(requestRole), "admin") {
		return "admin"
	}
	return "user"
}

func quotaForImportRole(role string, requested models.UserQuota, roleSyncEnabled bool) models.UserQuota {
	if strings.EqualFold(strings.TrimSpace(role), "admin") && (roleSyncEnabled || requested.IsDefaultForRole("user")) {
		requested.ApplyResourceValues(models.DefaultQuotaForRole("admin"))
	}
	return requested
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown LDAP import error"
}

func isUserNotFound(err error) bool {
	return err != nil && err.Error() == "user not found"
}

func ldapImportEmail(username, externalID string) string {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return ""
	}
	// LDAP directories commonly omit mail, and the same uid can occur in
	// several OUs. Deriving the fallback from the DN keeps those imports
	// globally unique without using a mutable counter.
	digest := sha256.Sum256([]byte(strings.TrimSpace(externalID)))
	return fmt.Sprintf("%s-%s@import.clawmanager.local", username, hex.EncodeToString(digest[:4]))
}

// ListUsersRequest represents a list users request
type ListUsersRequest struct {
	Page  int `form:"page,default=1"`
	Limit int `form:"limit,default=20"`
}

// UpdateUserRequest represents an update user request
type UpdateUserRequest struct {
	Email    string `json:"email" binding:"omitempty,email"`
	IsActive *bool  `json:"is_active" binding:"omitempty"`
}

// UpdateRoleRequest represents an update role request
type UpdateRoleRequest struct {
	Role string `json:"role" binding:"required,oneof=admin user"`
}

// UpdateQuotaRequest represents an update quota request
type UpdateQuotaRequest struct {
	MaxInstances int     `json:"max_instances" binding:"min=0"`
	MaxCPUCores  float64 `json:"max_cpu_cores" binding:"min=0"`
	MaxMemoryGB  int     `json:"max_memory_gb" binding:"min=0"`
	MaxStorageGB int `json:"max_storage_gb" binding:"min=0"`
	MaxGPUCount  int `json:"max_gpu_count" binding:"min=0"`
}

// CreateUserRequest represents a create user request (admin only)
type CreateUserRequest struct {
	Username     string `json:"username" binding:"required,min=3,max=32,alphanum"`
	Email        string `json:"email" binding:"required,email"`
	Password     string `json:"password" binding:"omitempty,min=8"`
	Role         string `json:"role" binding:"required,oneof=admin user"`
	AuthProvider string `json:"auth_provider" binding:"omitempty,oneof=local ldap"`
}

type importUserResult struct {
	Line     int    `json:"line"`
	Username string `json:"username"`
	Error    string `json:"error"`
}

type importedUserCredential struct {
	Username        string `json:"username"`
	LoginAlias      string `json:"login_alias,omitempty"`
	Email           string `json:"email"`
	Role            string `json:"role"`
	AuthProvider    string `json:"auth_provider"`
	WarningCodes    []string `json:"warning_codes,omitempty"`
	MaxInstances    int     `json:"max_instances"`
	MaxCPUCores     float64 `json:"max_cpu_cores"`
	MaxMemoryGB     int     `json:"max_memory_gb"`
	MaxStorageGB    int    `json:"max_storage_gb"`
	MaxGPUCount     int    `json:"max_gpu_count"`
	InitialPassword string `json:"initial_password,omitempty"`
}

type updatedLDAPUser struct {
	Username     string `json:"username"`
	LoginAlias   string `json:"login_alias,omitempty"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	AuthProvider string `json:"auth_provider"`
}

var importUsernamePattern = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// ListUsers lists all users (admin only)
func (h *UserHandler) ListUsers(c *gin.Context) {
	var req ListUsersRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	// Calculate offset
	offset := (req.Page - 1) * req.Limit

	users, err := h.userService.ListUsers(offset, req.Limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get total count
	total, err := h.userService.CountUsers()
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"users": users,
		"total": total,
		"page":  req.Page,
		"limit": req.Limit,
	}

	utils.Success(c, http.StatusOK, "Users retrieved successfully", response)
}

// CreateUser creates a new user (admin only)
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if normalizeImportedAuthProvider(req.AuthProvider) == services.AuthProviderLDAP {
		utils.Error(c, http.StatusBadRequest, "LDAP users must be imported from LDAP")
		return
	}

	user, err := h.userService.CreateUserWithProvider(req.Username, req.Email, req.Password, req.Role, req.AuthProvider)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusCreated, "User created successfully", user)
}

// ImportUsers imports users from a CSV file (admin only)
func (h *UserHandler) ImportUsers(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "User import file is required")
		return
	}
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".csv") {
		utils.Error(c, http.StatusBadRequest, "Only CSV files are supported")
		return
	}

	src, err := file.Open()
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Failed to open import file")
		return
	}
	defer src.Close()

	reader := csv.NewReader(src)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1

	rows, err := reader.ReadAll()
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid CSV format")
		return
	}

	if len(rows) == 0 {
		utils.Error(c, http.StatusBadRequest, "Import file is empty")
		return
	}

	results := make([]importUserResult, 0)
	createdUsers := make([]importedUserCredential, 0)

	headerMap := buildImportHeaderMap(rows[0])
	if len(headerMap) == 0 {
		utils.Error(c, http.StatusBadRequest, "Import file must include the required CSV headers")
		return
	}

	requiredHeaders := []string{"username", "role", "maxinstances", "maxcpucores", "maxmemorygb", "maxstoragegb"}
	for _, header := range requiredHeaders {
		if _, ok := headerMap[header]; !ok {
			utils.Error(c, http.StatusBadRequest, fmt.Sprintf("%s column is required", headerLabel(header)))
			return
		}
	}

	for i := 1; i < len(rows); i++ {
		lineNumber := i + 1
		fields := normalizeImportFields(rows[i])
		if len(fields) == 0 {
			continue
		}

		username := importFieldValue(fields, headerMap, "username")
		email := importFieldValue(fields, headerMap, "email")
		role := importFieldValue(fields, headerMap, "role")
		authProvider := normalizeImportedAuthProvider(importFieldValue(fields, headerMap, "authprovider"))
		externalID := importFieldValue(fields, headerMap, "externalid")
		password := importFieldValue(fields, headerMap, "password")
		maxInstances, parseErr := parseImportInt(fields, headerMap, "maxinstances", true)
		if parseErr != "" {
			results = append(results, importUserResult{Line: lineNumber, Username: username, Error: parseErr})
			continue
		}
		maxCPUCores, parseErr := parseImportFloat(fields, headerMap, "maxcpucores", true)
		if parseErr != "" {
			results = append(results, importUserResult{Line: lineNumber, Username: username, Error: parseErr})
			continue
		}
		maxMemoryGB, parseErr := parseImportInt(fields, headerMap, "maxmemorygb", true)
		if parseErr != "" {
			results = append(results, importUserResult{Line: lineNumber, Username: username, Error: parseErr})
			continue
		}
		maxStorageGB, parseErr := parseImportInt(fields, headerMap, "maxstoragegb", true)
		if parseErr != "" {
			results = append(results, importUserResult{Line: lineNumber, Username: username, Error: parseErr})
			continue
		}
		maxGPUCount, parseErr := parseImportInt(fields, headerMap, "maxgpucount", false)
		if parseErr != "" {
			results = append(results, importUserResult{Line: lineNumber, Username: username, Error: parseErr})
			continue
		}

		if email == "" && username != "" {
			if authProvider == services.AuthProviderLDAP {
				email = ldapImportEmail(username, externalID)
			} else {
				email = fmt.Sprintf("%s@import.clawmanager.local", strings.ToLower(username))
			}
		}

		if validationErr := validateImportedUser(username, email, password, role, authProvider, externalID); validationErr != "" {
			results = append(results, importUserResult{
				Line:     lineNumber,
				Username: username,
				Error:    validationErr,
			})
			continue
		}

		importQuota := quotaForImportRole(role, models.UserQuota{
			MaxInstances:  maxInstances,
			MaxCPUCores:   maxCPUCores,
			MaxMemoryGB:   maxMemoryGB,
			MaxStorageGB:  maxStorageGB,
			MaxGPUCount:   maxGPUCount,
		}, false)

		initialPassword := ""
		warningCodes := importWarningCodes(authProvider, password)
		if authProvider == services.AuthProviderLocal && password == "" {
			password = servicesDefaultPasswordForRole(role)
		}
		if authProvider == services.AuthProviderLocal {
			initialPassword = password
		}

		var user *models.User
		var createErr error
		if authProvider == services.AuthProviderLDAP {
			user, createErr = h.userService.CreateUserWithProviderAndExternalID(username, email, "", role, authProvider, externalID)
		} else {
			user, createErr = h.userService.CreateUserWithProvider(username, email, password, role, authProvider)
		}
		if createErr != nil {
			results = append(results, importUserResult{
				Line:     lineNumber,
				Username: username,
				Error:    createErr.Error(),
			})
			continue
		}

		if quotaErr := h.quotaService.UpdateUserQuota(user.ID, &importQuota); quotaErr != nil {
			results = append(results, importUserResult{
				Line:     lineNumber,
				Username: username,
				Error:    quotaErr.Error(),
			})
			continue
		}

		createdUsers = append(createdUsers, importedUserCredential{
			Username:        user.Username,
			LoginAlias:      stringPtrValue(user.LoginAlias),
			Email:           user.Email,
			Role:            user.Role,
			AuthProvider:    user.AuthProvider,
			WarningCodes:    warningCodes,
			MaxInstances:    importQuota.MaxInstances,
			MaxCPUCores:     importQuota.MaxCPUCores,
			MaxMemoryGB:     importQuota.MaxMemoryGB,
			MaxStorageGB:    importQuota.MaxStorageGB,
			MaxGPUCount:     importQuota.MaxGPUCount,
			InitialPassword: initialPassword,
		})
	}

	utils.Success(c, http.StatusCreated, "Users imported successfully", gin.H{
		"created_count": len(createdUsers),
		"failed_count":  len(results),
		"created_users": createdUsers,
		"errors":        results,
	})
}

func stringPtrValue(value *string) string {
	if value == nil { return "" }
	return *value
}

func normalizeImportFields(record []string) []string {
	fields := make([]string, 0, len(record))
	for _, field := range record {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			fields = append(fields, "")
			continue
		}
		fields = append(fields, trimmed)
	}
	return fields
}

func buildImportHeaderMap(record []string) map[string]int {
	headers := map[string]int{}
	for index, raw := range record {
		key := normalizeImportHeader(raw)
		switch key {
		case "username", "email", "role", "authprovider", "externalid", "password", "maxinstances", "maxcpucores", "maxmemorygb", "maxstoragegb", "maxgpucount":
			headers[key] = index
		}
	}
	return headers
}

func normalizeImportHeader(raw string) string {
	key := strings.ToLower(strings.TrimSpace(raw))
	replacer := strings.NewReplacer(" ", "", "_", "", "(", "", ")", "", "-", "")
	return replacer.Replace(key)
}

func importFieldValue(fields []string, headerMap map[string]int, key string) string {
	index, ok := headerMap[key]
	if !ok || index >= len(fields) {
		return ""
	}
	return strings.TrimSpace(fields[index])
}

func normalizeImportedAuthProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", services.AuthProviderLocal:
		return services.AuthProviderLocal
	case services.AuthProviderLDAP:
		return services.AuthProviderLDAP
	default:
		return provider
	}
}

func importWarningCodes(authProvider, password string) []string {
	if authProvider == services.AuthProviderLDAP && strings.TrimSpace(password) != "" {
		return []string{"ldap_password_ignored"}
	}
	return nil
}

func validateImportedUser(username, email, password, role, authProvider, externalID string) string {
	if len(username) < 3 || len(username) > 32 {
		return "Username must be between 3 and 32 characters"
	}
	if !importUsernamePattern.MatchString(username) {
		return "Username must be alphanumeric"
	}
	if email == "" || !strings.Contains(email, "@") {
		return "Email must be a valid email"
	}
	if role != "admin" && role != "user" {
		return "Role must be admin or user"
	}
	if authProvider != services.AuthProviderLocal && authProvider != services.AuthProviderLDAP {
		return "Auth Provider must be local or ldap"
	}
	if authProvider == services.AuthProviderLDAP && strings.TrimSpace(externalID) == "" {
		return "External ID is required for LDAP users"
	}
	if authProvider == services.AuthProviderLocal && password != "" && len(password) < 8 {
		return "Password must be at least 8 characters"
	}
	if authProvider == services.AuthProviderLocal && strings.HasPrefix(strings.ToLower(strings.TrimSpace(username)), "ldap_") {
		return "local usernames cannot start with ldap_"
	}
	return ""
}

func parseImportInt(fields []string, headerMap map[string]int, key string, required bool) (int, string) {
	value := importFieldValue(fields, headerMap, key)
	if value == "" {
		if required {
			return 0, fmt.Sprintf("%s is required", headerLabel(key))
		}
		return 0, ""
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Sprintf("%s must be a non-negative integer", headerLabel(key))
	}
	return parsed, ""
}

func parseImportFloat(fields []string, headerMap map[string]int, key string, required bool) (float64, string) {
	value := importFieldValue(fields, headerMap, key)
	if value == "" {
		if required {
			return 0, fmt.Sprintf("%s is required", headerLabel(key))
		}
		return 0, ""
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Sprintf("%s must be a non-negative number", headerLabel(key))
	}
	return parsed, ""
}

func headerLabel(key string) string {
	switch key {
	case "username":
		return "Username"
	case "role":
		return "Role"
	case "authprovider":
		return "Auth Provider"
	case "externalid":
		return "External ID"
	case "maxinstances":
		return "Max Instances"
	case "maxcpucores":
		return "Max CPU Cores"
	case "maxmemorygb":
		return "Max Memory (GB)"
	case "maxstoragegb":
		return "Max Storage (GB)"
	case "maxgpucount":
		return "Max GPU Count"
	default:
		return key
	}
}

func servicesDefaultPasswordForRole(role string) string {
	return services.DefaultPasswordForRole(role)
}

// GetUser gets a user by ID
func (h *UserHandler) GetUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Get current user ID from context
	currentUserID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")

	// Only admin or the user themselves can view user details
	if userRole != "admin" && currentUserID.(int) != id {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	user, err := h.userService.GetUserByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if user == nil {
		utils.Error(c, http.StatusNotFound, "User not found")
		return
	}

	utils.Success(c, http.StatusOK, "User retrieved successfully", user)
}

// UpdateUser updates a user
func (h *UserHandler) UpdateUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	// Get current user ID from context
	currentUserID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")

	// Users can update their own profile. Admins may update another user's
	// active status so LDAP whitelist entries can be restored after disable.
	isSelfUpdate := currentUserID.(int) == id
	isAdminStatusUpdate := userRole == "admin" && req.IsActive != nil && req.Email == ""
	if !isSelfUpdate && !isAdminStatusUpdate {
		utils.Error(c, http.StatusForbidden, "Can only update your own profile or user status as admin")
		return
	}

	user := &models.User{
		ID:       id,
		Email:    req.Email,
		IsActive: true,
	}

	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}

	if err := h.userService.UpdateUser(user); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "User updated successfully", user)
}

// DeleteUser deletes a user (admin only)
func (h *UserHandler) DeleteUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Prevent admin from deleting themselves
	currentUserID, _ := c.Get("userID")
	if currentUserID.(int) == id {
		utils.Error(c, http.StatusBadRequest, "Cannot delete yourself")
		return
	}

	if err := h.userService.DeleteUser(id); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "User deleted successfully", nil)
}

// UpdateRole updates a user's role (admin only)
func (h *UserHandler) UpdateRole(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var req UpdateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	// Prevent admin from changing their own role
	currentUserID, _ := c.Get("userID")
	if currentUserID.(int) == id {
		utils.Error(c, http.StatusBadRequest, "Cannot change your own role")
		return
	}

	if err := h.userService.UpdateUserRole(id, req.Role); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "User role updated successfully", nil)
}

// GetUserQuota gets a user's quota
func (h *UserHandler) GetUserQuota(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Get current user ID from context
	currentUserID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")

	// Only admin or the user themselves can view quota
	if userRole != "admin" && currentUserID.(int) != id {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	quota, err := h.quotaService.GetUserQuota(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Quota retrieved successfully", quota)
}

// UpdateUserQuota updates a user's quota (admin only)
func (h *UserHandler) UpdateUserQuota(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var req UpdateQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	quota := &models.UserQuota{
		MaxInstances: req.MaxInstances,
		MaxCPUCores:  req.MaxCPUCores,
		MaxMemoryGB:  req.MaxMemoryGB,
		MaxStorageGB: req.MaxStorageGB,
		MaxGPUCount:  req.MaxGPUCount,
	}

	if err := h.quotaService.UpdateUserQuota(id, quota); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Quota updated successfully", quota)
}
