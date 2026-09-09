package handlers

import (
	"net/http"

	"lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type FolderHandler struct {
	folderService *services.FolderService
}

func NewFolderHandler(folderService *services.FolderService) *FolderHandler {
	return &FolderHandler{folderService: folderService}
}

func (h *FolderHandler) ListFolders(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	search := c.QueryParam("search")
	if search == "" {
		search = c.QueryParam("q")
	}
	folders, err := h.folderService.ListFolders(accountID, search)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, folders)
}

type CreateFolderRequest struct {
	Name string `json:"name"`
}

func (h *FolderHandler) CreateFolder(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	var req CreateFolderRequest
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Folder name is required", nil)
	}

	folder, err := h.folderService.CreateFolder(accountID, actor.ID, req.Name, actor)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "CREATE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusCreated, folder)
}

type RenameFolderRequest struct {
	Name string `json:"name"`
}

func (h *FolderHandler) RenameFolder(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	folderIDStr := c.Param("id")
	folderID, err := uuid.Parse(folderIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid folder ID", nil)
	}

	var req RenameFolderRequest
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Folder name is required", nil)
	}

	folder, err := h.folderService.RenameFolder(accountID, folderID, req.Name, actor)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "RENAME_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, folder)
}

func (h *FolderHandler) DeleteFolder(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	folderIDStr := c.Param("id")
	folderID, err := uuid.Parse(folderIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid folder ID", nil)
	}

	if err := h.folderService.DeleteFolder(accountID, folderID, actor); err != nil {
		return response.Error(c, http.StatusBadRequest, "DELETE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "Folder deleted successfully"})
}
