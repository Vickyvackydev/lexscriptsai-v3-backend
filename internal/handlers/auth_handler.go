package handlers

import (
	"net/http"
	"strings"

	"lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/models"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthHandler struct {
	authService       *services.AuthService
	permissionService *services.PermissionService
	db                *gorm.DB
}

func NewAuthHandler(authService *services.AuthService, permissionService *services.PermissionService, db *gorm.DB) *AuthHandler {
	return &AuthHandler{
		authService:       authService,
		permissionService: permissionService,
		db:                db,
	}
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(c echo.Context) error {
	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid request body", nil)
	}

	if req.Email == "" || req.Password == "" {
		return response.Error(c, http.StatusBadRequest, "MISSING_CREDENTIALS", "Email and password are required", nil)
	}

	ip := c.RealIP()
	ua := c.Request().UserAgent()

	result, err := h.authService.Login(req.Email, req.Password, ip, ua)
	if err != nil {
		return response.Error(c, http.StatusUnauthorized, "AUTH_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, result)
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

func (h *AuthHandler) Refresh(c echo.Context) error {
	var req RefreshRequest
	if err := c.Bind(&req); err != nil || req.RefreshToken == "" {
		return response.Error(c, http.StatusBadRequest, "MISSING_REFRESH_TOKEN", "Refresh token is required", nil)
	}

	tokens, err := h.authService.RefreshToken(req.RefreshToken)
	if err != nil {
		return response.Error(c, http.StatusUnauthorized, "REFRESH_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, tokens)
}

func (h *AuthHandler) GetMe(c echo.Context) error {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		return response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated", nil)
	}

	perms := []string{"view", "edit", "upload", "export", "share", "collaborate", "delete", "shared_files"}
	if user.SystemRole == models.RoleSubAccount && user.AccountID != nil && h.permissionService != nil {
		p, err := h.permissionService.GetMemberPermission(*user.AccountID, user.ID)
		if err == nil && p != nil && p.Permissions != nil {
			perms = []string(p.Permissions)
		}
	}

	return response.Success(c, http.StatusOK, map[string]interface{}{
		"user":        user,
		"isAdmin":     user.SystemRole == "admin",
		"permissions": perms,
	})
}

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

func (h *AuthHandler) ForgotPassword(c echo.Context) error {
	var req ForgotPasswordRequest
	if err := c.Bind(&req); err != nil || req.Email == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Email address is required", nil)
	}

	if err := h.authService.ForgotPassword(req.Email); err != nil {
		return response.Error(c, http.StatusInternalServerError, "FORGOT_PASSWORD_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{
		"message": "If that email address is registered, a password reset link has been dispatched.",
	})
}

type ResetPasswordRequest struct {
	Email       string `json:"email"`
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

func (h *AuthHandler) ResetPassword(c echo.Context) error {
	var req ResetPasswordRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid request body", nil)
	}

	if err := h.authService.ResetPassword(req.Email, req.Token, req.NewPassword); err != nil {
		return response.Error(c, http.StatusBadRequest, "RESET_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{
		"message": "Your password has been successfully updated. You may now sign in.",
	})
}

func (h *AuthHandler) GetPreferences(c echo.Context) error {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		return response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated", nil)
	}

	var dbUser models.User
	if err := h.db.First(&dbUser, "id = ?", user.ID).Error; err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", "User not found", nil)
	}

	return response.Success(c, http.StatusOK, map[string]interface{}{
		"autoSave":     dbUser.AutoSave,
		"autoDownload": dbUser.AutoDownload,
	})
}

type UpdatePreferencesRequest struct {
	AutoSave     *bool `json:"autoSave"`
	AutoDownload *bool `json:"autoDownload"`
}

func (h *AuthHandler) UpdatePreferences(c echo.Context) error {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		return response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated", nil)
	}

	var req UpdatePreferencesRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid preferences payload", nil)
	}

	updates := map[string]interface{}{}
	if req.AutoSave != nil {
		updates["auto_save"] = *req.AutoSave
	}
	if req.AutoDownload != nil {
		updates["auto_download"] = *req.AutoDownload
	}

	if len(updates) > 0 {
		if err := h.db.Model(&models.User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
			return response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", "Failed to update preferences", nil)
		}
	}

	var updatedUser models.User
	h.db.First(&updatedUser, "id = ?", user.ID)

	return response.Success(c, http.StatusOK, map[string]interface{}{
		"autoSave":     updatedUser.AutoSave,
		"autoDownload": updatedUser.AutoDownload,
		"message":      "Preferences updated successfully",
	})
}

type UpdateProfileRequest struct {
	Name      string `json:"name"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

func (h *AuthHandler) UpdateProfile(c echo.Context) error {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		return response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated", nil)
	}

	var req UpdateProfileRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid profile payload", nil)
	}

	updates := map[string]interface{}{}
	if strings.TrimSpace(req.Name) != "" {
		updates["name"] = strings.TrimSpace(req.Name)
	}
	if strings.TrimSpace(req.FirstName) != "" {
		updates["first_name"] = strings.TrimSpace(req.FirstName)
	}
	if strings.TrimSpace(req.LastName) != "" {
		updates["last_name"] = strings.TrimSpace(req.LastName)
	}

	if len(updates) > 0 && h.db != nil {
		if err := h.db.Model(&models.User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
			return response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", "Failed to update profile", nil)
		}
	}

	var updatedUser models.User
	h.db.First(&updatedUser, "id = ?", user.ID)

	return response.Success(c, http.StatusOK, map[string]interface{}{
		"user":    updatedUser,
		"message": "Profile updated successfully",
	})
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (h *AuthHandler) ChangePassword(c echo.Context) error {
	user := middleware.GetCurrentUser(c)
	if user == nil {
		return response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Not authenticated", nil)
	}

	var req ChangePasswordRequest
	if err := c.Bind(&req); err != nil || req.CurrentPassword == "" || req.NewPassword == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Current password and new password are required", nil)
	}

	var dbUser models.User
	if err := h.db.First(&dbUser, "id = ?", user.ID).Error; err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", "User not found", nil)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(dbUser.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_CREDENTIALS", "Current password is incorrect", nil)
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "HASH_FAILED", "Failed to hash new password", nil)
	}

	if err := h.db.Model(&models.User{}).Where("id = ?", user.ID).Update("password_hash", string(hashed)).Error; err != nil {
		return response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", "Failed to update password", nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{
		"message": "Password changed successfully",
	})
}

