package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"lexscriptsai-v3-backend/internal/auth"
	"lexscriptsai-v3-backend/internal/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthService struct {
	db           *gorm.DB
	tokenService *auth.TokenService
	auditService *AuditService
	emailService *EmailService
	frontendURL  string
}

func NewAuthService(db *gorm.DB, tokenService *auth.TokenService, auditService *AuditService, emailService *EmailService, frontendURL string) *AuthService {
	return &AuthService{
		db:           db,
		tokenService: tokenService,
		auditService: auditService,
		emailService: emailService,
		frontendURL:  frontendURL,
	}
}

type LoginResponse struct {
	User        models.User     `json:"user"`
	Account     *models.Account `json:"account,omitempty"`
	Tokens      auth.TokenPair  `json:"tokens"`
	IsAdmin     bool            `json:"isAdmin"`
	Permissions []string        `json:"permissions"`
}

func (s *AuthService) Login(email string, password string, ip string, userAgent string) (*LoginResponse, error) {
	var user models.User
	if err := s.db.Where("LOWER(email) = LOWER(?)", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("invalid email or password")
		}
		return nil, err
	}

	if user.Status != models.StatusActive {
		return nil, errors.New("account inactive, please contact administrator")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		s.auditService.Log(user.AccountID, user.Name, user.ID, user.Name, "login_failed", "user", user.ID.String(), "Invalid password attempt", ip, userAgent)
		return nil, errors.New("invalid email or password")
	}

	var account *models.Account
	if user.AccountID != nil {
		var acc models.Account
		if err := s.db.Where("id = ?", *user.AccountID).First(&acc).Error; err == nil {
			account = &acc
		}
	}

	tokens, err := s.tokenService.GenerateTokenPair(&user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tokens: %w", err)
	}

	s.auditService.Log(user.AccountID, user.Name, user.ID, user.Name, "login", "user", user.ID.String(), "Successful login", ip, userAgent)

	perms := []string{"view", "edit", "upload", "export", "share", "collaborate", "delete", "shared_files"}
	if user.SystemRole == models.RoleSubAccount && user.AccountID != nil {
		var memberPerm models.MemberPermission
		if err := s.db.Where("user_id = ?", user.ID).First(&memberPerm).Error; err == nil {
			if memberPerm.Permissions != nil {
				perms = []string(memberPerm.Permissions)
			}
		}
	}

	return &LoginResponse{
		User:        user,
		Account:     account,
		Tokens:      *tokens,
		IsAdmin:     user.SystemRole == models.RoleAdmin,
		Permissions: perms,
	}, nil
}

func (s *AuthService) RefreshToken(refreshToken string) (*auth.TokenPair, error) {
	claims, err := s.tokenService.ValidateToken(refreshToken, "refresh")
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	var user models.User
	if err := s.db.Where("id = ?", claims.UserID).First(&user).Error; err != nil {
		return nil, errors.New("user not found")
	}

	if user.Status != models.StatusActive {
		return nil, errors.New("account inactive")
	}

	return s.tokenService.GenerateTokenPair(&user)
}

func (s *AuthService) ForgotPassword(email string) error {
	var user models.User
	if err := s.db.Where("LOWER(email) = LOWER(?)", strings.TrimSpace(email)).First(&user).Error; err != nil {
		// Do not reveal email existence to prevent user enumeration
		return nil
	}

	// 1. Create an in-app Admin Notification for this password reset request
	notif := models.Notification{
		AccountID:     user.AccountID,
		UserID:        &user.ID,
		Type:          "password_reset_request",
		Title:         "Password Reset Requested",
		Message:       fmt.Sprintf("%s (%s) requested a password reset. Please update their credentials.", user.Name, user.Email),
		ActionURL:     "/admin/accounts",
		ActorName:     user.Name,
		RecipientRole: "admin",
		Status:        "pending",
		UserEmail:     user.Email,
	}
	s.db.Create(&notif)

	// 2. Log audit trail
	s.auditService.Log(user.AccountID, user.Name, user.ID, user.Name, "password_reset_requested", "user", user.ID.String(), fmt.Sprintf("User %s (%s) requested password reset", user.Name, user.Email), "", "")

	// 3. Send email to user confirming request received and admin notified
	go s.emailService.SendPasswordResetRequestAcknowledged(user.Email, user.Name)

	// 4. Send direct alert email to admin@lexscriptsai.com
	nowFormatted := time.Now().Format("02 Jan 2006, 15:04 MST")
	go s.emailService.SendAdminPasswordResetAlert("admin@lexscriptsai.com", user.Name, user.Email, nowFormatted, s.frontendURL+"/admin/accounts")

	return nil
}

func (s *AuthService) ResetPassword(email, token, newPassword string) error {
	if email == "" || token == "" || newPassword == "" {
		return errors.New("email, token, and newPassword are required")
	}

	if len(newPassword) < 8 {
		return errors.New("password must be at least 8 characters long")
	}

	var user models.User
	if err := s.db.Where("LOWER(email) = LOWER(?) AND reset_password_token = ?", email, token).First(&user).Error; err != nil {
		return errors.New("invalid or expired password reset link")
	}

	if user.ResetPasswordExpiresAt == nil || user.ResetPasswordExpiresAt.Before(time.Now().UTC()) {
		return errors.New("password reset link has expired. Please request a new one")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return errors.New("failed to process password")
	}

	user.PasswordHash = string(hash)
	user.ResetPasswordToken = ""
	user.ResetPasswordExpiresAt = nil

	if err := s.db.Save(&user).Error; err != nil {
		return errors.New("failed to update password")
	}

	s.auditService.Log(user.AccountID, user.Name, user.ID, user.Name, "password_reset", "user", user.ID.String(), "Password successfully reset via email link", "", "")

	return nil
}
