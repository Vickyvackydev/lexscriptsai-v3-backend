package handlers

import (
	"html"
	"net/http"
	"strings"

	"lexscriptsai-v3-backend/internal/auth"
	"lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/models"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type CollabHandler struct {
	collabService *services.CollabService
	aiService     *services.AIService
	tokenService  *auth.TokenService
	db            *gorm.DB
}

func NewCollabHandler(collabService *services.CollabService, aiService *services.AIService, tokenService *auth.TokenService, db *gorm.DB) *CollabHandler {
	return &CollabHandler{
		collabService: collabService,
		aiService:     aiService,
		tokenService:  tokenService,
		db:            db,
	}
}

func (h *CollabHandler) HandleWebSocket(c echo.Context) error {
	transcriptID := c.Param("id")
	if transcriptID == "" {
		return response.Error(c, http.StatusBadRequest, "MISSING_ID", "Transcript ID required", nil)
	}

	token := c.QueryParam("token")
	if token == "" {
		authHeader := c.Request().Header.Get("Authorization")
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			token = authHeader[7:]
		}
	}

	if token == "" {
		return response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Missing authentication token", nil)
	}

	claims, err := h.tokenService.ValidateToken(token, "access")
	if err != nil {
		return response.Error(c, http.StatusUnauthorized, "INVALID_TOKEN", err.Error(), nil)
	}

	return h.collabService.HandleWS(c.Response().Writer, c.Request(), transcriptID, claims)
}

type ShareRequest struct {
	Email string                     `json:"email"`
	Role  models.TranscriptShareRole `json:"role"`
}

func (h *CollabHandler) ShareTranscript(c echo.Context) error {
	idStr := c.Param("id")
	transcriptID, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid transcript ID", nil)
	}

	userID := middleware.GetUserID(c)

	var req ShareRequest
	if err := c.Bind(&req); err != nil || req.Email == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Email is required", nil)
	}

	share, err := h.collabService.ShareTranscript(transcriptID, userID, req.Email, req.Role)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "SHARE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, share)
}

func (h *CollabHandler) ListCollaborators(c echo.Context) error {
	idStr := c.Param("id")
	transcriptID, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid transcript ID", nil)
	}

	shares, err := h.collabService.ListCollaborators(transcriptID)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, shares)
}

func (h *CollabHandler) RemoveCollaborator(c echo.Context) error {
	idStr := c.Param("id")
	transcriptID, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid transcript ID", nil)
	}

	targetUserStr := c.Param("userId")
	targetUserID, err := uuid.Parse(targetUserStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_USER_ID", "Invalid target user ID", nil)
	}

	userID := middleware.GetUserID(c)
	if err := h.collabService.RemoveCollaborator(transcriptID, userID, targetUserID); err != nil {
		return response.Error(c, http.StatusInternalServerError, "REMOVE_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, map[string]bool{"removed": true})
}

func (h *CollabHandler) ListSharedTranscripts(c echo.Context) error {
	userID := middleware.GetUserID(c)
	items, err := h.collabService.ListSharedWithUser(userID)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error(), nil)
	}
	if items == nil {
		items = make([]services.SharedTranscriptItem, 0)
	}

	return response.Success(c, http.StatusOK, items)
}

type TranslateRequest struct {
	TargetLanguage string `json:"targetLanguage"`
	BankIndex      *int   `json:"bankIndex,omitempty"`
}

func (h *CollabHandler) TranslateTranscript(c echo.Context) error {
	idStr := c.Param("id")
	transcriptID, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid transcript ID", nil)
	}

	var req TranslateRequest
	if err := c.Bind(&req); err != nil || req.TargetLanguage == "" {
		return response.Error(c, http.StatusBadRequest, "INVALID_BODY", "Target language is required", nil)
	}

	var transcript models.Transcript
	if err := h.db.First(&transcript, "id = ?", transcriptID).Error; err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", "Transcript not found", nil)
	}

	updatedBanks := transcript.SpeakerBanks

	translateBank := func(bank *models.SpeakerBank) {
		if len(bank.Words) == 0 {
			return
		}
		var words []string
		for _, w := range bank.Words {
			words = append(words, w.Text)
		}
		fullText := strings.Join(words, " ")
		translated, err := h.aiService.TranslateText(fullText, req.TargetLanguage)
		if err == nil && translated != "" {
			translated = html.UnescapeString(translated)
			tWords := strings.Fields(translated)
			if len(tWords) > 0 {
				newWords := make([]models.Word, len(tWords))
				start := bank.Words[0].StartTime
				end := bank.Words[len(bank.Words)-1].EndTime
				step := 0.2
				if end > start && len(tWords) > 0 {
					step = (end - start) / float64(len(tWords))
				}
				for i, tw := range tWords {
					newWords[i] = models.Word{
						Text:      tw,
						StartTime: start + float64(i)*step,
						EndTime:   start + float64(i+1)*step,
					}
				}
				bank.Words = newWords
			}
		}
	}

	if req.BankIndex != nil {
		idx := *req.BankIndex
		if idx >= 0 && idx < len(updatedBanks) {
			translateBank(&updatedBanks[idx])
		}
	} else {
		for bIdx := range updatedBanks {
			translateBank(&updatedBanks[bIdx])
		}
	}

	return response.Success(c, http.StatusOK, map[string]interface{}{
		"targetLanguage": req.TargetLanguage,
		"speakerBanks":   updatedBanks,
	})
}

type SummaryRequest struct {
	SummaryFormat string `json:"summaryFormat,omitempty"`
	Length        string `json:"length,omitempty"`
}

func (h *CollabHandler) GenerateSummary(c echo.Context) error {
	idStr := c.Param("id")
	transcriptID, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid transcript ID", nil)
	}

	var transcript models.Transcript
	if err := h.db.First(&transcript, "id = ?", transcriptID).Error; err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", "Transcript not found", nil)
	}

	summary, err := h.aiService.GenerateCourtSummary(&transcript)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "SUMMARY_FAILED", err.Error(), nil)
	}

	return response.Success(c, http.StatusOK, summary)
}
