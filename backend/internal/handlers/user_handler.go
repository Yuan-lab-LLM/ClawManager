package handlers

import (
	"encoding/csv"
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
	userService  services.UserService
	quotaService services.QuotaService
}

// NewUserHandler creates a new user handler
func NewUserHandler(userService services.UserService, quotaService services.QuotaService) *UserHandler {
	return &UserHandler{
		userService:  userService,
		quotaService: quotaService,
	}
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
	MaxInstances int `json:"max_instances" binding:"min=0"`
	MaxCPUCores  int `json:"max_cpu_cores" binding:"min=0"`
	MaxMemoryGB  int `json:"max_memory_gb" binding:"min=0"`
	MaxStorageGB int `json:"max_storage_gb" binding:"min=0"`
	MaxGPUCount  int `json:"max_gpu_count" binding:"min=0"`
}

// CreateUserRequest represents a create user request (admin only)
type CreateUserRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32,alphanum"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"omitempty,min=8"`
	Role     string `json:"role" binding:"required,oneof=admin user"`
}

type importUserResult struct {
	Line     int    `json:"line"`
	Username string `json:"username"`
	Error    string `json:"error"`
}

type importedUserCredential struct {
	Username        string `json:"username"`
	Email           string `json:"email"`
	Role            string `json:"role"`
	MaxInstances    int    `json:"max_instances"`
	MaxCPUCores     int    `json:"max_cpu_cores"`
	MaxMemoryGB     int    `json:"max_memory_gb"`
	MaxStorageGB    int    `json:"max_storage_gb"`
	MaxGPUCount     int    `json:"max_gpu_count"`
	InitialPassword string `json:"initial_password"`
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

	user, err := h.userService.CreateUser(req.Username, req.Email, req.Password, req.Role)
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
		password := importFieldValue(fields, headerMap, "password")
		maxInstances, parseErr := parseImportInt(fields, headerMap, "maxinstances", true)
		if parseErr != "" {
			results = append(results, importUserResult{Line: lineNumber, Username: username, Error: parseErr})
			continue
		}
		maxCPUCores, parseErr := parseImportInt(fields, headerMap, "maxcpucores", true)
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

		if password == "" {
			password = servicesDefaultPasswordForRole(role)
		}
		if email == "" && username != "" {
			email = fmt.Sprintf("%s@import.clawmanager.local", strings.ToLower(username))
		}

		if validationErr := validateImportedUser(username, email, password, role); validationErr != "" {
			results = append(results, importUserResult{
				Line:     lineNumber,
				Username: username,
				Error:    validationErr,
			})
			continue
		}

		user, createErr := h.userService.CreateUser(username, email, password, role)
		if createErr != nil {
			results = append(results, importUserResult{
				Line:     lineNumber,
				Username: username,
				Error:    createErr.Error(),
			})
			continue
		}

		if quotaErr := h.quotaService.UpdateUserQuota(user.ID, &models.UserQuota{
			MaxInstances: maxInstances,
			MaxCPUCores:  maxCPUCores,
			MaxMemoryGB:  maxMemoryGB,
			MaxStorageGB: maxStorageGB,
			MaxGPUCount:  maxGPUCount,
		}); quotaErr != nil {
			results = append(results, importUserResult{
				Line:     lineNumber,
				Username: username,
				Error:    quotaErr.Error(),
			})
			continue
		}

		createdUsers = append(createdUsers, importedUserCredential{
			Username:        user.Username,
			Email:           user.Email,
			Role:            user.Role,
			MaxInstances:    maxInstances,
			MaxCPUCores:     maxCPUCores,
			MaxMemoryGB:     maxMemoryGB,
			MaxStorageGB:    maxStorageGB,
			MaxGPUCount:     maxGPUCount,
			InitialPassword: password,
		})
	}

	utils.Success(c, http.StatusCreated, "Users imported successfully", gin.H{
		"created_count": len(createdUsers),
		"failed_count":  len(results),
		"created_users": createdUsers,
		"errors":        results,
	})
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
		case "username", "email", "role", "password", "maxinstances", "maxcpucores", "maxmemorygb", "maxstoragegb", "maxgpucount":
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

func validateImportedUser(username, email, password, role string) string {
	if len(username) < 3 || len(username) > 32 {
		return "Username must be between 3 and 32 characters"
	}
	if !importUsernamePattern.MatchString(username) {
		return "Username must be alphanumeric"
	}
	if email == "" || !strings.Contains(email, "@") {
		return "Email must be a valid email"
	}
	if len(password) < 8 {
		return "Password must be at least 8 characters"
	}
	if role != "admin" && role != "user" {
		return "Role must be admin or user"
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

func headerLabel(key string) string {
	switch key {
	case "username":
		return "Username"
	case "role":
		return "Role"
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

	// Users can only update their own profile
	if currentUserID.(int) != id {
		utils.Error(c, http.StatusForbidden, "Can only update your own profile")
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
