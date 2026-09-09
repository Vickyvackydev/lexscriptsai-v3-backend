package handlers

import (
	"fmt"
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

type NotificationHandler struct {
	notificationService *services.NotificationService
	tokenService        *auth.TokenService
	db                  *gorm.DB
}

func NewNotificationHandler(notificationService *services.NotificationService, tokenService *auth.TokenService, db *gorm.DB) *NotificationHandler {
	return &NotificationHandler{
		notificationService: notificationService,
		tokenService:        tokenService,
		db:                  db,
	}
}

func (h *NotificationHandler) ListUserNotifications(c echo.Context) error {
	user := middleware.GetCurrentUser(c)
	accountID := middleware.GetAccountID(c)

	var userID uuid.UUID
	var email string
	if user != nil {
		userID = user.ID
		email = user.Email
	}

	notifs, err := h.notificationService.ListUserNotifications(userID, email, accountID)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "FETCH_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, notifs)
}

func (h *NotificationHandler) MarkNotificationRead(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid notification ID", nil)
	}

	if err := h.notificationService.MarkRead(id); err != nil {
		return response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, map[string]string{"message": "Notification marked as read"})
}

func (h *NotificationHandler) MarkAllRead(c echo.Context) error {
	user := middleware.GetCurrentUser(c)
	accountID := middleware.GetAccountID(c)

	var userID uuid.UUID
	var email string
	if user != nil {
		userID = user.ID
		email = user.Email
	}

	if err := h.notificationService.MarkAllRead(userID, email, accountID); err != nil {
		return response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, map[string]string{"message": "All notifications marked as read"})
}

func (h *NotificationHandler) DeleteNotification(c echo.Context) error {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid notification ID", nil)
	}

	if err := h.notificationService.DeleteNotification(id); err != nil {
		return response.Error(c, http.StatusInternalServerError, "DELETE_FAILED", err.Error(), nil)
	}
	return response.Success(c, http.StatusOK, map[string]string{"message": "Notification deleted"})
}

// HandleWebSocket handles real-time notification push connections
func (h *NotificationHandler) HandleWebSocket(c echo.Context) error {
	tokenString := c.QueryParam("token")
	if tokenString == "" {
		authHeader := c.Request().Header.Get("Authorization")
		if parts := strings.Split(authHeader, " "); len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			tokenString = parts[1]
		}
	}

	if tokenString == "" {
		return c.String(http.StatusUnauthorized, "Missing auth token for notifications WebSocket")
	}

	claims, err := h.tokenService.ValidateToken(tokenString, "access")
	if err != nil {
		return c.String(http.StatusUnauthorized, "Invalid token: "+err.Error())
	}

	return h.notificationService.HandleWS(c.Response().Writer, c.Request(), claims)
}

type TranscriptionWebhookPayload struct {
	TranscriptID string               `json:"transcriptId"`
	Status       string               `json:"status"` // "completed" or "failed"
	ErrorMessage string               `json:"errorMessage,omitempty"`
	Duration     int                  `json:"duration,omitempty"`
	WordCount    int                  `json:"wordCount,omitempty"`
	SpeakerBanks []models.SpeakerBank `json:"speakerBanks,omitempty"`
}

// TranscriptionWebhook handles external webhook callbacks for transcription completions and failures
func (h *NotificationHandler) TranscriptionWebhook(c echo.Context) error {
	var payload TranscriptionWebhookPayload
	if err := c.Bind(&payload); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_PAYLOAD", "Invalid webhook payload", nil)
	}

	tID, err := uuid.Parse(payload.TranscriptID)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_TRANSCRIPT_ID", "Invalid transcript ID", nil)
	}

	var transcript models.Transcript
	if err := h.db.First(&transcript, "id = ?", tID).Error; err != nil {
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", "Transcript not found", nil)
	}

	statusLower := strings.ToLower(payload.Status)
	if statusLower == "completed" {
		updates := map[string]interface{}{
			"status": models.TranscriptCompleted,
		}
		if payload.Duration > 0 {
			updates["duration"] = payload.Duration
		}
		if payload.WordCount > 0 {
			updates["word_count"] = payload.WordCount
		}
		if len(payload.SpeakerBanks) > 0 {
			updates["speaker_banks"] = models.SpeakerBanks(payload.SpeakerBanks)
		}
		h.db.Model(&transcript).Updates(updates)

		notif := models.Notification{
			AccountID:     &transcript.AccountID,
			UserID:        &transcript.OwnerID,
			Type:          "processing_complete",
			Title:         "Transcription Completed",
			Message:       fmt.Sprintf("Your transcript '%s' has finished processing.", transcript.Title),
			ActionURL:     fmt.Sprintf("/transcripts/%s", transcript.ID),
			RecipientRole: "user",
			Read:          false,
		}
		h.notificationService.CreateNotification(&notif)

		return response.Success(c, http.StatusOK, map[string]string{"message": "Transcription marked completed and notification dispatched"})
	} else if statusLower == "failed" {
		h.db.Model(&transcript).Update("status", models.TranscriptFailed)

		notif := models.Notification{
			AccountID:     &transcript.AccountID,
			UserID:        &transcript.OwnerID,
			Type:          "processing_failed",
			Title:         "Transcription Notice",
			Message:       fmt.Sprintf("Processing could not be completed for '%s'.", transcript.Title),
			ActionURL:     "/transcripts",
			RecipientRole: "user",
			Read:          false,
		}
		h.notificationService.CreateNotification(&notif)

		return response.Success(c, http.StatusOK, map[string]string{"message": "Transcription marked failed and notification dispatched"})
	}

	return response.Error(c, http.StatusBadRequest, "UNKNOWN_STATUS", "Status must be completed or failed", nil)
}
