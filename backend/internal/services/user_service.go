package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
)

// UserService defines the interface for user operations
type UserService interface {
	CreateUser(username, email, password, role string) (*models.User, error)
	CreateUserWithProvider(username, email, password, role, authProvider string) (*models.User, error)
	CreateUserWithProviderAndExternalID(username, email, password, role, authProvider, externalID string) (*models.User, error)
	GetUserByID(id int) (*models.User, error)
	GetUserByUsername(username string) (*models.User, error)
	GetUserByLoginAlias(authProvider, loginAlias string) (*models.User, error)
	GetUserByExternalIdentity(authProvider, externalID string) (*models.User, error)
	EnsureLDAPLoginAlias(externalID string) (*models.User, error)
	GetUserByEmail(email string) (*models.User, error)
	ListUsers(offset, limit int) ([]models.User, error)
	CountUsers() (int, error)
	UpdateUser(user *models.User) error
	DeleteUser(id int) error
	UpdateUserRole(id int, role string) error
	CreateDefaultQuota(userID int) error
}

func defaultPasswordForRole(role string) string {
	return DefaultPasswordForRole(role)
}

// userService implements UserService
type userService struct {
	userRepo  repository.UserRepository
	quotaRepo repository.QuotaRepository
}

// NewUserService creates a new user service
func NewUserService(userRepo repository.UserRepository, quotaRepo repository.QuotaRepository) UserService {
	return &userService{
		userRepo:  userRepo,
		quotaRepo: quotaRepo,
	}
}

// CreateUser creates a new user (admin only)
func (s *userService) CreateUser(username, email, password, role string) (*models.User, error) {
	return s.CreateUserWithProvider(username, email, password, role, AuthProviderLocal)
}

// CreateUserWithProvider creates a user without an external identity. LDAP
// users must use CreateUserWithProviderAndExternalID so their DN is stored.
func (s *userService) CreateUserWithProvider(username, email, password, role, authProvider string) (*models.User, error) {
	return s.CreateUserWithProviderAndExternalID(username, email, password, role, authProvider, "")
}

func (s *userService) CreateUserWithProviderAndExternalID(username, email, password, role, authProvider, externalID string) (*models.User, error) {
	authProvider = normalizeAuthProvider(authProvider)
	if authProvider == AuthProviderLocal && isReservedLocalUsername(username) {
		return nil, errors.New("local usernames cannot start with ldap_")
	}
	if authProvider == AuthProviderLDAP && strings.TrimSpace(externalID) == "" {
		return nil, errors.New("LDAP users must be imported from LDAP")
	}
	if authProvider == AuthProviderLocal && password == "" {
		password = defaultPasswordForRole(role)
	}

	var existingUser *models.User
	var err error
	if authProvider == AuthProviderLDAP {
		existingUser, err = s.userRepo.GetByExternalIdentity(authProvider, strings.TrimSpace(externalID))
	} else {
		// Local usernames remain unique. LDAP users may share a uid across OUs.
		existingUser, err = s.userRepo.GetByAuthProviderUsername(authProvider, username)
	}
	if err != nil { return nil, fmt.Errorf("failed to check user identity: %w", err) }
	if existingUser != nil {
		if authProvider == AuthProviderLocal {
			return nil, errors.New("username already exists")
		}
		return nil, errors.New("user already exists")
	}

	// Check if email already exists
	existingUser, err = s.userRepo.GetByEmail(email)
	if err != nil {
		return nil, fmt.Errorf("failed to check email: %w", err)
	}
	if existingUser != nil {
		return nil, errors.New("email already exists")
	}

	var passwordHash string
	if authProvider == AuthProviderLDAP {
		passwordHash = enterprisePasswordMarker(authProvider)
	} else {
		// Hash password
		var err error
		passwordHash, err = utils.HashPassword(password)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password: %w", err)
		}
	}

	var alias string
	if authProvider == AuthProviderLDAP {
		alias, err = s.allocateLDAPLoginAlias(username, externalID)
		if err != nil { return nil, err }
	}

	// The availability check above is only advisory. The unique index is the
	// authority when two imports race for the same alias.
	const maxAliasRetries = 8
	for attempt := 0; ; attempt++ {
		user := &models.User{
			Username:     username,
			Email:        email,
			PasswordHash: passwordHash,
			Role:         role,
			AuthProvider: authProvider,
			IsActive:     true,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if authProvider == AuthProviderLDAP {
			user.LoginAlias = stringPtr(alias)
			user.ExternalID = stringPtr(strings.TrimSpace(externalID))
		}
		if createErr := s.userRepo.Create(user); createErr != nil {
			if errors.Is(createErr, repository.ErrUserUsernameConflict) {
				return nil, errors.New("username already exists")
			}
			if authProvider == AuthProviderLDAP && errors.Is(createErr, repository.ErrUserLoginAliasConflict) && attempt+1 < maxAliasRetries {
				// If this was the same directory entry being imported by two
				// requests, make the operation idempotent. Otherwise allocate the
				// next alias for the competing LDAP entry.
				if existing, lookupErr := s.userRepo.GetByExternalIdentity(authProvider, strings.TrimSpace(externalID)); lookupErr == nil && existing != nil {
					return existing, nil
				}
				alias, err = s.allocateLDAPLoginAlias(username, externalID)
				if err != nil {
					return nil, err
				}
				continue
			}
			return nil, fmt.Errorf("failed to create user: %w", createErr)
		}

		// Create the ordinary quota first, then promote it when this is an
		// administrator. This keeps custom quota updates separate from role
		// defaults and preserves the existing repository contract.
		if _, err := s.quotaRepo.CreateDefaultQuota(user.ID); err != nil {
			return nil, fmt.Errorf("failed to create default quota: %w", err)
		}
		if err := SyncDefaultQuotaForRole(s.quotaRepo, user.ID, role); err != nil {
			return nil, err
		}

		return user, nil
	}
}

// GetUserByID gets a user by ID
func (s *userService) GetUserByID(id int) (*models.User, error) {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, errors.New("user not found")
	}
	return user, nil
}

// GetUserByUsername gets a user by username
func (s *userService) GetUserByUsername(username string) (*models.User, error) {
	// Compatibility wrapper for callers that still have a local username. New
	// business logic should use a provider, alias, or external identity query.
	user, err := s.userRepo.GetByAuthProviderUsername(AuthProviderLocal, username)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, errors.New("user not found")
	}
	return user, nil
}

func (s *userService) GetUserByLoginAlias(authProvider, loginAlias string) (*models.User, error) {
	user, err := s.userRepo.GetByLoginAlias(authProvider, loginAlias)
	if err != nil { return nil, fmt.Errorf("failed to get user by login alias: %w", err) }
	if user == nil { return nil, errors.New("user not found") }
	return user, nil
}

func (s *userService) GetUserByExternalIdentity(authProvider, externalID string) (*models.User, error) {
	user, err := s.userRepo.GetByExternalIdentity(authProvider, externalID)
	if err != nil { return nil, fmt.Errorf("failed to get user by external identity: %w", err) }
	if user == nil { return nil, errors.New("user not found") }
	return user, nil
}

func (s *userService) EnsureLDAPLoginAlias(externalID string) (*models.User, error) {
	externalID = strings.TrimSpace(externalID)
	user, err := s.userRepo.GetByExternalIdentity(AuthProviderLDAP, externalID)
	if err != nil { return nil, fmt.Errorf("failed to get LDAP user: %w", err) }
	if user == nil { return nil, errors.New("user not found") }
	if user.LoginAlias != nil && strings.TrimSpace(*user.LoginAlias) != "" { return user, nil }
	alias, err := s.allocateLDAPLoginAlias(user.Username, externalID)
	if err != nil { return nil, err }
	user.LoginAlias = &alias
	user.UpdatedAt = time.Now()
	if err := s.userRepo.Update(user); err != nil { return nil, fmt.Errorf("failed to save LDAP login alias: %w", err) }
	return user, nil
}

func (s *userService) allocateLDAPLoginAlias(username, externalID string) (string, error) {
	base := "ldap_" + sanitizeLDAPAliasPart(username)
	sameUIDCount, err := s.userRepo.CountByAuthProviderUsername(AuthProviderLDAP, username)
	if err != nil {
		return "", fmt.Errorf("failed to count LDAP users with the same uid: %w", err)
	}
	if sameUIDCount <= 1 {
		if existing, err := s.userRepo.GetByLoginAlias(AuthProviderLDAP, base); err != nil {
			return "", fmt.Errorf("failed to check LDAP login alias: %w", err)
		} else if existing == nil {
			return base, nil
		}
	}

	suffix := ldapAliasOU(externalID)
	if suffix == "" { suffix = "user" }
	base += "_" + suffix
	for index := 1; ; index++ {
		alias := base
		if index > 1 { alias = fmt.Sprintf("%s_%d", base, index) }
		existing, err := s.userRepo.GetByLoginAlias(AuthProviderLDAP, alias)
		if err != nil { return "", fmt.Errorf("failed to check LDAP login alias: %w", err) }
		if existing == nil { return alias, nil }
	}
}

func sanitizeLDAPAliasPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') { b.WriteRune(r) } else { b.WriteByte('_') }
	}
	result := strings.Trim(b.String(), "_")
	if result == "" { return "user" }
	return result
}

func ldapAliasOU(externalID string) string {
	for _, part := range strings.Split(externalID, ",")[1:] {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) == 2 && strings.EqualFold(pair[0], "ou") { return sanitizeLDAPAliasPart(pair[1]) }
	}
	return ""
}

func (s *userService) GetUserByEmail(email string) (*models.User, error) {
	user, err := s.userRepo.GetByEmail(email)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, errors.New("user not found")
	}
	return user, nil
}

// ListUsers lists all users with pagination
func (s *userService) ListUsers(offset, limit int) ([]models.User, error) {
	users, err := s.userRepo.List(offset, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	return users, nil
}

// CountUsers counts all users
func (s *userService) CountUsers() (int, error) {
	count, err := s.userRepo.Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}
	return count, nil
}

// UpdateUser updates a user
func (s *userService) UpdateUser(user *models.User) error {
	existingUser, err := s.userRepo.GetByID(user.ID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	if existingUser == nil {
		return errors.New("user not found")
	}

	// Update allowed fields
	if user.Email != "" {
		existingUser.Email = user.Email
	}
	existingUser.IsActive = user.IsActive
	existingUser.UpdatedAt = time.Now()

	if err := s.userRepo.Update(existingUser); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// DeleteUser deletes a user
func (s *userService) DeleteUser(id int) error {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return errors.New("user not found")
	}

	// Delete user's quota first
	if err := s.quotaRepo.DeleteByUserID(id); err != nil {
		return fmt.Errorf("failed to delete user quota: %w", err)
	}

	// Delete user
	if err := s.userRepo.Delete(id); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return nil
}

// UpdateUserRole updates a user's role
func (s *userService) UpdateUserRole(id int, role string) error {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return errors.New("user not found")
	}

	user.Role = role
	user.UpdatedAt = time.Now()

	if err := s.userRepo.Update(user); err != nil {
		return fmt.Errorf("failed to update user role: %w", err)
	}
	if err := SyncDefaultQuotaForRole(s.quotaRepo, id, role); err != nil {
		return err
	}

	return nil
}

// CreateDefaultQuota creates default quota for a user
func (s *userService) CreateDefaultQuota(userID int) error {
	_, err := s.quotaRepo.CreateDefaultQuota(userID)
	return err
}
