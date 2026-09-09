package handlers

import (
	"net/http"

	"lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type CauseListHandler struct {
	causeListService *services.CauseListService
}

func NewCauseListHandler(causeListService *services.CauseListService) *CauseListHandler {
	return &CauseListHandler{causeListService: causeListService}
}

func (h *CauseListHandler) ListCauseLists(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	search := c.QueryParam("search")
	if search == "" {
		search = c.QueryParam("q")
	}
	lists, err := h.causeListService.ListCauseLists(accountID, search)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, lists)
}

type CreateCauseListRequest struct {
	Name     string     `json:"name"`
	FolderID *uuid.UUID `json:"folderId"`
}

func (h *CauseListHandler) CreateCauseList(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	var req CreateCauseListRequest
	if err := c.Bind(&req); err != nil || req.Name == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Cause list name is required", nil)
	}

	list, err := h.causeListService.CreateCauseList(accountID, actor.ID, req.Name, req.FolderID, actor)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "CREATE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusCreated, list)
}

func (h *CauseListHandler) ListMatters(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	date := c.QueryParam("date")
	search := c.QueryParam("search")
	if search == "" {
		search = c.QueryParam("q")
	}

	var folderID *uuid.UUID
	if fStr := c.QueryParam("folderId"); fStr != "" {
		if fid, err := uuid.Parse(fStr); err == nil {
			folderID = &fid
		}
	}

	items, err := h.causeListService.ListMatters(accountID, date, folderID, search)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, items)
}

func (h *CauseListHandler) CreateMatter(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	var req services.CreateMatterInput
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid matter request body", nil)
	}

	matter, err := h.causeListService.CreateMatter(accountID, req, actor)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "CREATE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusCreated, matter)
}

func (h *CauseListHandler) AdjournMatter(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	idStr := c.Param("id")
	matterID, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid matter ID", nil)
	}

	var req services.AdjournMatterInput
	if err := c.Bind(&req); err != nil || req.NewDate == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "newDate is required", nil)
	}

	matter, err := h.causeListService.AdjournMatter(accountID, matterID, req, actor)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "ADJOURN_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, matter)
}

func (h *CauseListHandler) DeleteMatter(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	idStr := c.Param("id")
	matterID, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid matter ID", nil)
	}

	if err := h.causeListService.DeleteMatter(accountID, matterID, actor); err != nil {
		return response.Error(c, http.StatusBadRequest, "DELETE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{"message": "Matter deleted successfully"})
}
