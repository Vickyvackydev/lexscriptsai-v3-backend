package handlers

import (
	"net/http"

	"lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type RecycleBinHandler struct {
	recycleBinService *services.RecycleBinService
}

func NewRecycleBinHandler(recycleBinService *services.RecycleBinService) *RecycleBinHandler {
	return &RecycleBinHandler{recycleBinService: recycleBinService}
}

func (h *RecycleBinHandler) ListRecycleBin(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	items, err := h.recycleBinService.ListRecycleBin(accountID)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, items)
}

func (h *RecycleBinHandler) Restore(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid transcript ID", nil)
	}

	if err := h.recycleBinService.Restore(accountID, id, actor); err != nil {
		return response.Error(c, http.StatusBadRequest, "RESTORE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "Transcript restored successfully"})
}

func (h *RecycleBinHandler) PermanentDelete(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid transcript ID", nil)
	}

	if err := h.recycleBinService.PermanentDelete(accountID, id, actor); err != nil {
		return response.Error(c, http.StatusInternalServerError, "PURGE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "Transcript permanently purged"})
}
