package handlers

import (
	"net/http"

	"lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/models"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type PermissionHandler struct {
	permissionService *services.PermissionService
}

func NewPermissionHandler(permissionService *services.PermissionService) *PermissionHandler {
	return &PermissionHandler{permissionService: permissionService}
}

func (h *PermissionHandler) ListAccountMembers(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	user := middleware.GetCurrentUser(c)

	var excludeID uuid.UUID
	if user != nil {
		excludeID = user.ID
		if accountID == uuid.Nil {
			var acc models.Account
			if err := h.permissionService.GetDB().Where("owner_email = ?", user.Email).First(&acc).Error; err == nil {
				accountID = acc.ID
			}
		}
	}

	if qID := c.QueryParam("accountId"); qID != "" {
		if parsed, err := uuid.Parse(qID); err == nil {
			accountID = parsed
		}
	}

	// If still Nil and user is admin, grab the first account that has sub-accounts
	if accountID == uuid.Nil && user != nil && user.SystemRole == models.RoleAdmin {
		var firstSub models.User
		if err := h.permissionService.GetDB().Where("system_role = ? AND account_id IS NOT NULL", models.RoleSubAccount).First(&firstSub).Error; err == nil && firstSub.AccountID != nil {
			accountID = *firstSub.AccountID
		}
	}

	members, err := h.permissionService.ListAccountMembers(accountID, excludeID)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "FETCH_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, members)
}

func (h *PermissionHandler) GetMemberPermissions(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	targetUserIDStr := c.Param("userId")
	targetUserID, err := uuid.Parse(targetUserIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid member user ID", nil)
	}

	perm, err := h.permissionService.GetMemberPermission(accountID, targetUserID)
	if err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, perm)
}

func (h *PermissionHandler) UpdateMemberPermissions(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	targetUserIDStr := c.Param("userId")
	targetUserID, err := uuid.Parse(targetUserIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid member user ID", nil)
	}

	var input services.UpdatePermissionInput
	if err := c.Bind(&input); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid request body", nil)
	}

	perm, err := h.permissionService.UpdateMemberPermission(accountID, targetUserID, input, actor)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "UPDATE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, perm)
}
