package handlers

import (
	"net/http"
	"strconv"

	"lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type TranscriptHandler struct {
	transcriptService *services.TranscriptService
	db                *gorm.DB
}

func NewTranscriptHandler(transcriptService *services.TranscriptService, db *gorm.DB) *TranscriptHandler {
	return &TranscriptHandler{transcriptService: transcriptService, db: db}
}

func (h *TranscriptHandler) ListTranscripts(c echo.Context) error {
	accountID := middleware.GetAccountID(c)

	var folderID *uuid.UUID
	if fStr := c.QueryParam("folderId"); fStr != "" {
		if fid, err := uuid.Parse(fStr); err == nil {
			folderID = &fid
		}
	}

	page, _ := strconv.Atoi(c.QueryParam("page"))
	pageSize, _ := strconv.Atoi(c.QueryParam("pageSize"))

	filter := services.ListTranscriptsFilter{
		FolderID: folderID,
		Status:   c.QueryParam("status"),
		Source:   c.QueryParam("source"),
		Search:   c.QueryParam("search"),
		Page:     page,
		PageSize: pageSize,
	}

	transcripts, total, err := h.transcriptService.ListTranscripts(accountID, filter)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}

	return response.Paginated(c, transcripts, filter.Page, filter.PageSize, total)
}

func (h *TranscriptHandler) GetTranscript(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid transcript ID", nil)
	}

	transcript, err := middleware.CheckTranscriptAccess(c, h.db, id, "view")
	if err != nil {
		if he, ok := err.(*echo.HTTPError); ok {
			return response.Error(c, he.Code, "ACCESS_DENIED", he.Message.(string), nil)
		}
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", "Transcript not found", nil)
	}

	if transcript != nil && h.transcriptService != nil {
		h.transcriptService.SignAudioURL(transcript)
	}

	return response.Success(c, http.StatusOK, transcript)
}

func (h *TranscriptHandler) RetryTranscript(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid transcript ID", nil)
	}

	actor := middleware.GetCurrentUser(c)
	accountID := middleware.GetAccountID(c)

	transcript, err := h.transcriptService.RetryTranscript(accountID, id, actor)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "RETRY_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, transcript)
}

func (h *TranscriptHandler) DownloadAudio(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid transcript ID", nil)
	}

	accountID := middleware.GetAccountID(c)
	audioURL, err := h.transcriptService.GetAudioDownloadURL(accountID, id)
	if err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
	}

	return c.Redirect(http.StatusTemporaryRedirect, audioURL)
}

func (h *TranscriptHandler) CreateTranscript(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	actor := middleware.GetCurrentUser(c)

	var input services.CreateTranscriptInput
	if err := c.Bind(&input); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid request body", nil)
	}

	transcript, err := h.transcriptService.CreateTranscript(accountID, actor.ID, input, actor)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "CREATE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusCreated, transcript)
}

func (h *TranscriptHandler) UpdateTranscript(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid transcript ID", nil)
	}

	_, err = middleware.CheckTranscriptAccess(c, h.db, id, "edit")
	if err != nil {
		if he, ok := err.(*echo.HTTPError); ok {
			return response.Error(c, he.Code, "ACCESS_DENIED", he.Message.(string), nil)
		}
		return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Edit permission denied", nil)
	}

	var input services.UpdateTranscriptInput
	if err := c.Bind(&input); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid request body", nil)
	}

	actor := middleware.GetCurrentUser(c)
	accountID := middleware.GetAccountID(c)

	updated, err := h.transcriptService.UpdateTranscript(accountID, id, input, actor)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, updated)
}

func (h *TranscriptHandler) DeleteTranscript(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid transcript ID", nil)
	}

	_, err = middleware.CheckTranscriptAccess(c, h.db, id, "delete")
	if err != nil {
		if he, ok := err.(*echo.HTTPError); ok {
			return response.Error(c, he.Code, "ACCESS_DENIED", he.Message.(string), nil)
		}
		return response.Error(c, http.StatusForbidden, "FORBIDDEN", "Delete permission denied", nil)
	}

	actor := middleware.GetCurrentUser(c)
	accountID := middleware.GetAccountID(c)

	if err := h.transcriptService.MoveToTrash(accountID, id, actor); err != nil {
		return response.Error(c, http.StatusInternalServerError, "DELETE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{
		"message": "Transcript moved to recycle bin successfully",
	})
}
