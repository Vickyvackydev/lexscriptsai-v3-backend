package middleware

import (
	"net/http"
	"strings"

	"lexscriptsai-v3-backend/internal/auth"
	"lexscriptsai-v3-backend/internal/models"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

const (
	ContextKeyUser             = "user"
	ContextKeyUserID           = "userId"
	ContextKeyAccountID        = "accountId"
	ContextKeySystemRole       = "systemRole"
	ContextKeyProfessionalRole = "professionalRole"
)

func RequireAuth(tokenService *auth.TokenService, db *gorm.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Missing authorization header", nil)
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				return response.Error(c, http.StatusUnauthorized, "INVALID_TOKEN_FORMAT", "Invalid authorization header format. Expected 'Bearer <token>'", nil)
			}

			tokenString := parts[1]
			claims, err := tokenService.ValidateToken(tokenString, "access")
			if err != nil {
				return response.Error(c, http.StatusUnauthorized, "INVALID_TOKEN", err.Error(), nil)
			}

			var user models.User
			if err := db.Where("id = ?", claims.UserID).First(&user).Error; err != nil {
				return response.Error(c, http.StatusUnauthorized, "USER_NOT_FOUND", "Authenticated user no longer exists", nil)
			}

			if user.Status != models.StatusActive {
				return response.Error(c, http.StatusForbidden, "ACCOUNT_INACTIVE", "Your account has been deactivated or suspended", nil)
			}

			accountID := uuid.Nil
			if user.AccountID != nil {
				accountID = *user.AccountID
			} else {
				// If user is an owner, look up their account by owner_email
				var acc models.Account
				if err := db.Where("owner_email = ?", user.Email).First(&acc).Error; err == nil {
					accountID = acc.ID
					user.AccountID = &acc.ID
					db.Model(&user).Update("account_id", acc.ID)
				}
			}

			c.Set(ContextKeyUser, &user)
			c.Set(ContextKeyUserID, user.ID)
			c.Set(ContextKeyAccountID, accountID)
			c.Set(ContextKeySystemRole, user.SystemRole)
			c.Set(ContextKeyProfessionalRole, user.ProfessionalRole)

			return next(c)
		}
	}
}

func GetCurrentUser(c echo.Context) *models.User {
	if val, ok := c.Get(ContextKeyUser).(*models.User); ok {
		return val
	}
	return nil
}

func GetUserID(c echo.Context) uuid.UUID {
	if val, ok := c.Get(ContextKeyUserID).(uuid.UUID); ok {
		return val
	}
	return uuid.Nil
}

func GetAccountID(c echo.Context) uuid.UUID {
	if val, ok := c.Get(ContextKeyAccountID).(uuid.UUID); ok {
		return val
	}
	if val, ok := c.Get(ContextKeyAccountID).(*uuid.UUID); ok && val != nil {
		return *val
	}
	return uuid.Nil
}

func GetSystemRole(c echo.Context) models.SystemRole {
	if val, ok := c.Get(ContextKeySystemRole).(models.SystemRole); ok {
		return val
	}
	return ""
}
