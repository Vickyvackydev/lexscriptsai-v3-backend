package handlers

import (
	"net/http"
	"time"

	"lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/models"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type UploadHandler struct {
	storageService *services.StorageService
	db             *gorm.DB
}

func NewUploadHandler(storageService *services.StorageService, db *gorm.DB) *UploadHandler {
	return &UploadHandler{storageService: storageService, db: db}
}

type GenerateUploadURLRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
}

func (h *UploadHandler) GenerateUploadURL(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	ownerID := middleware.GetUserID(c)

	var req GenerateUploadURLRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Invalid request body", nil)
	}

	if req.Filename == "" || req.ContentType == "" {
		return response.Error(c, http.StatusBadRequest, "MISSING_FIELDS", "Filename and contentType are required", nil)
	}

	res, err := h.storageService.GenerateUploadSignedURL(accountID, ownerID, req.Filename, req.ContentType, req.SizeBytes)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "UPLOAD_URL_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, res)
}

func (h *UploadHandler) DirectUpload(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	ownerID := middleware.GetUserID(c)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "MISSING_FILE", "Form field 'file' is required", nil)
	}

	fileRecord, publicURL, err := h.storageService.UploadMultipart(c.Request().Context(), fileHeader, accountID, ownerID)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "UPLOAD_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]interface{}{
		"file":     fileRecord,
		"audioUrl": publicURL,
	})
}

func (h *UploadHandler) GetDownloadSignedURL(c echo.Context) error {
	accountID := middleware.GetAccountID(c)
	role := middleware.GetSystemRole(c)

	fileIDStr := c.Param("id")
	fileID, err := uuid.Parse(fileIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_UUID", "Invalid file ID", nil)
	}

	var file models.File
	query := h.db.Where("id = ?", fileID)
	if role != models.RoleAdmin {
		query = query.Where("account_id = ?", accountID)
	}

	if err := query.First(&file).Error; err != nil {
		return response.Error(c, http.StatusNotFound, "FILE_NOT_FOUND", "File not found or access denied", nil)
	}

	signedURL, err := h.storageService.GenerateDownloadSignedURL(&file, 1*time.Hour)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "SIGNED_URL_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]string{
		"signedUrl": signedURL,
	})
}
