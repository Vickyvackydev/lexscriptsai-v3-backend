package middleware

import (
	"net/http"

	"lexscriptsai-v3-backend/internal/models"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/labstack/echo/v4"
)

func RequireAdmin() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			role := GetSystemRole(c)
			if role != models.RoleAdmin {
				return response.Error(c, http.StatusForbidden, "ADMIN_ACCESS_REQUIRED", "This action requires system administrator privileges", nil)
			}
			return next(c)
		}
	}
}

func RequireAccountOwner() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			role := GetSystemRole(c)
			if role != models.RoleOwner && role != models.RoleAdmin {
				return response.Error(c, http.StatusForbidden, "OWNER_ACCESS_REQUIRED", "This action requires Account Owner privileges", nil)
			}
			return next(c)
		}
	}
}
