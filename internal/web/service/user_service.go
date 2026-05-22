package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ssl-manager/ssl-manager/internal/model"
	"github.com/ssl-manager/ssl-manager/internal/web/repository"
	"golang.org/x/crypto/bcrypt"
)

// UserService handles user management business logic.
type UserService struct {
	userRepo    *repository.UserRepository
	authService *AuthService
}

// NewUserService creates a new UserService.
func NewUserService(userRepo *repository.UserRepository, authService *AuthService) *UserService {
	return &UserService{
		userRepo:    userRepo,
		authService: authService,
	}
}

// CreateUserInput holds the input for creating a new user.
type CreateUserInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UpdateUserRoleInput holds the input for updating a user's role.
type UpdateUserRoleInput struct {
	Role string `json:"role"`
}

// ResetPasswordInput holds the input for resetting a user's password.
type ResetPasswordInput struct {
	NewPassword string `json:"new_password"`
}

// List returns all users.
func (s *UserService) List(ctx context.Context) ([]*model.User, error) {
	users, err := s.userRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	return users, nil
}

// Create creates a new user.
func (s *UserService) Create(ctx context.Context, input CreateUserInput) (*model.User, error) {
	if input.Username == "" {
		return nil, errors.New("username is required")
	}
	if input.Password == "" {
		return nil, errors.New("password is required")
	}
	if len(input.Password) < 6 {
		return nil, errors.New("password must be at least 6 characters")
	}
	if input.Role == "" {
		input.Role = "user"
	}
	if input.Role != "admin" && input.Role != "user" {
		return nil, errors.New("role must be 'admin' or 'user'")
	}

	// Check if username already exists
	existing, _ := s.userRepo.GetByUsername(ctx, input.Username)
	if existing != nil {
		return nil, errors.New("username already exists")
	}

	user := &model.User{
		Username:     input.Username,
		PasswordHash: input.Password, // UserRepository.Create will hash this
		Role:         input.Role,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// UpdateRole updates a user's role.
func (s *UserService) UpdateRole(ctx context.Context, id string, input UpdateUserRoleInput) (*model.User, error) {
	if input.Role == "" {
		return nil, errors.New("role is required")
	}
	if input.Role != "admin" && input.Role != "user" {
		return nil, errors.New("role must be 'admin' or 'user'")
	}

	// Verify user exists
	_, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("user not found: %s", id)
	}

	updates := map[string]interface{}{
		"role": input.Role,
	}
	if err := s.userRepo.Update(ctx, id, updates); err != nil {
		return nil, fmt.Errorf("failed to update user role: %w", err)
	}

	return s.userRepo.GetByID(ctx, id)
}

// Disable disables a user account and invalidates their sessions.
func (s *UserService) Disable(ctx context.Context, id string) error {
	// Verify user exists
	_, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("user not found: %s", id)
	}

	if err := s.userRepo.Disable(ctx, id); err != nil {
		return fmt.Errorf("failed to disable user: %w", err)
	}

	// Invalidate all sessions for this user so existing tokens are rejected
	s.authService.InvalidateUserSessions(id)

	return nil
}

// ResetPassword resets a user's password and invalidates their sessions.
func (s *UserService) ResetPassword(ctx context.Context, id string, input ResetPasswordInput) error {
	if input.NewPassword == "" {
		return errors.New("new_password is required")
	}
	if len(input.NewPassword) < 6 {
		return errors.New("password must be at least 6 characters")
	}

	// Verify user exists
	_, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("user not found: %s", id)
	}

	// Hash the new password
	hash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	if err := s.userRepo.UpdatePassword(ctx, id, string(hash)); err != nil {
		return fmt.Errorf("failed to reset password: %w", err)
	}

	// Invalidate all sessions for this user
	s.authService.InvalidateUserSessions(id)

	return nil
}
