package repository

import (
	"errors"
	"fmt"
	"strings"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// ErrUserLoginAliasConflict indicates that another request claimed the LDAP
// login alias between the availability check and the insert.
var ErrUserLoginAliasConflict = errors.New("user login alias conflict")

// ErrUserUsernameConflict indicates that another request claimed the local
// username between the availability check and the insert.
var ErrUserUsernameConflict = errors.New("user username conflict")

// UserRepository defines the interface for user data operations
type UserRepository interface {
	Create(user *models.User) error
	GetByID(id int) (*models.User, error)
	GetByUsername(username string) (*models.User, error)
	GetByAuthProviderUsername(authProvider, username string) (*models.User, error)
	CountByAuthProviderUsername(authProvider, username string) (int, error)
	GetByLoginAlias(authProvider, loginAlias string) (*models.User, error)
	GetByEmail(email string) (*models.User, error)
	GetByExternalIdentity(authProvider, externalID string) (*models.User, error)
	Update(user *models.User) error
	Delete(id int) error
	List(offset, limit int) ([]models.User, error)
	Count() (int, error)
}

// userRepository implements UserRepository
type userRepository struct {
	sess db.Session
}

// NewUserRepository creates a new user repository
func NewUserRepository(sess db.Session) UserRepository {
	return &userRepository{sess: sess}
}

// Create creates a new user
func (r *userRepository) Create(user *models.User) error {
	res, err := r.sess.Collection("users").Insert(user)
	if err != nil {
		if isLoginAliasConflict(err) {
			return fmt.Errorf("%w: %v", ErrUserLoginAliasConflict, err)
		}
		if isUsernameConflict(err) {
			return fmt.Errorf("%w: %v", ErrUserUsernameConflict, err)
		}
		return fmt.Errorf("failed to create user: %w", err)
	}
	// Get the last insert ID
	user.ID = int(res.ID().(int64))
	return nil
}

// GetByID gets a user by ID
func (r *userRepository) GetByID(id int) (*models.User, error) {
	var user models.User
	err := r.sess.Collection("users").Find(db.Cond{"id": id}).One(&user)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by ID: %w", err)
	}
	return &user, nil
}

// GetByUsername gets a user by username
func (r *userRepository) GetByUsername(username string) (*models.User, error) {
	var user models.User
	err := r.sess.Collection("users").Find(db.Cond{"username": username}).One(&user)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}
	return &user, nil
}

func isLoginAliasConflict(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "uk_users_provider_login_alias") ||
		(strings.Contains(message, "duplicate entry") && strings.Contains(message, "login_alias"))
}

func isUsernameConflict(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "uk_users_local_username") ||
		(strings.Contains(message, "duplicate entry") && strings.Contains(message, "local_username_key"))
}

func (r *userRepository) GetByAuthProviderUsername(authProvider, username string) (*models.User, error) {
	var user models.User
	err := r.sess.Collection("users").Find(db.Cond{"auth_provider": authProvider, "username": username}).One(&user)
	if err != nil {
		if err == db.ErrNoMoreRows { return nil, nil }
		return nil, fmt.Errorf("failed to get user by auth provider and username: %w", err)
	}
	return &user, nil
}

func (r *userRepository) CountByAuthProviderUsername(authProvider, username string) (int, error) {
	count, err := r.sess.Collection("users").Find(db.Cond{"auth_provider": authProvider, "username": username}).Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count users by auth provider and username: %w", err)
	}
	return int(count), nil
}

func (r *userRepository) GetByLoginAlias(authProvider, loginAlias string) (*models.User, error) {
	var user models.User
	err := r.sess.Collection("users").Find(db.Cond{"auth_provider": authProvider, "login_alias": loginAlias}).One(&user)
	if err != nil {
		if err == db.ErrNoMoreRows { return nil, nil }
		return nil, fmt.Errorf("failed to get user by login alias: %w", err)
	}
	return &user, nil
}

// GetByEmail gets a user by email
func (r *userRepository) GetByEmail(email string) (*models.User, error) {
	var user models.User
	err := r.sess.Collection("users").Find(db.Cond{"email": email}).One(&user)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}
	return &user, nil
}

// GetByExternalIdentity gets a user by enterprise identity provider and external ID.
func (r *userRepository) GetByExternalIdentity(authProvider, externalID string) (*models.User, error) {
	var user models.User
	err := r.sess.Collection("users").Find(db.Cond{
		"auth_provider": authProvider,
		"external_id":   externalID,
	}).One(&user)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by external identity: %w", err)
	}
	return &user, nil
}

// Update updates a user
func (r *userRepository) Update(user *models.User) error {
	err := r.sess.Collection("users").Find(db.Cond{"id": user.ID}).Update(user)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

// Delete deletes a user by ID
func (r *userRepository) Delete(id int) error {
	err := r.sess.Collection("users").Find(db.Cond{"id": id}).Delete()
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

// List returns a list of users with pagination
func (r *userRepository) List(offset, limit int) ([]models.User, error) {
	var users []models.User
	err := r.sess.Collection("users").Find().OrderBy("id").Offset(offset).Limit(limit).All(&users)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	return users, nil
}

// Count returns the total number of users
func (r *userRepository) Count() (int, error) {
	count, err := r.sess.Collection("users").Find().Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}
	return int(count), nil
}
