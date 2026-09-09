package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"lexscriptsai-v3-backend/internal/models"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func RequirePermission(db *gorm.DB, action string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			role := GetSystemRole(c)
			if role == models.RoleAdmin || role == models.RoleOwner {
				return next(c)
			}

			userID := GetUserID(c)
			var perm models.MemberPermission
			if err := db.Where("user_id = ?", userID).First(&perm).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					// Default all permissions to true until restricted by account owner
					return next(c)
				}
				return response.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to check member permissions", nil)
			}

			hasAction := false
			for _, p := range perm.Permissions {
				if strings.EqualFold(p, action) {
					hasAction = true
					break
				}
			}

			if !hasAction {
				return response.Error(c, http.StatusForbidden, "PERMISSION_DENIED", "You do not have permission to perform action: "+action, nil)
			}

			return next(c)
		}
	}
}

func CheckTranscriptAccess(c echo.Context, db *gorm.DB, transcriptID uuid.UUID, action string) (*models.Transcript, error) {
	accountID := GetAccountID(c)
	role := GetSystemRole(c)
	userID := GetUserID(c)

	var share models.TranscriptShare
	isShared := false
	if userID != uuid.Nil {
		isShared = db.Where("transcript_id = ? AND shared_with_id = ?", transcriptID, userID).First(&share).Error == nil
	}

	var transcript models.Transcript
	query := db.Where("id = ?", transcriptID)
	if role != models.RoleAdmin && !isShared {
		query = query.Where("account_id = ?", accountID)
	}

	if err := query.First(&transcript).Error; err != nil {
		return nil, echo.NewHTTPError(http.StatusNotFound, "Transcript not found")
	}

	if isShared {
		transcript.UserRole = string(share.Role)
		if share.Role == models.ShareRoleViewer && (action == "edit" || action == "delete") {
			return nil, echo.NewHTTPError(http.StatusForbidden, "Viewer role cannot edit or delete this transcript")
		}
		return &transcript, nil
	}

	if role == models.RoleAdmin || role == models.RoleOwner {
		transcript.UserRole = "editor"
		return &transcript, nil
	}

	var perm models.MemberPermission
	if err := db.Where("user_id = ?", userID).First(&perm).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Default all permissions to true until restricted by account owner
			transcript.UserRole = "editor"
			return &transcript, nil
		}
		return nil, echo.NewHTTPError(http.StatusForbidden, "No permissions assigned to this user")
	}

	hasEdit := false
	for _, p := range perm.Permissions {
		if strings.EqualFold(p, "edit") {
			hasEdit = true
			break
		}
	}
	if hasEdit {
		transcript.UserRole = "editor"
	} else {
		transcript.UserRole = "viewer"
	}

	if action != "" {
		hasAction := false
		for _, p := range perm.Permissions {
			if strings.EqualFold(p, action) {
				hasAction = true
				break
			}
		}
		if !hasAction {
			return nil, echo.NewHTTPError(http.StatusForbidden, "You do not have permission to "+action+" this transcript")
		}
	}

	if perm.AccessibleTranscriptIDs != "all" && perm.AccessibleTranscriptIDs != "" {
		var ids []string
		if err := json.Unmarshal([]byte(perm.AccessibleTranscriptIDs), &ids); err == nil {
			allowed := false
			targetStr := transcriptID.String()
			for _, id := range ids {
				if id == targetStr {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, echo.NewHTTPError(http.StatusForbidden, "You do not have access to this specific transcript")
			}
		}
	}

	return &transcript, nil
}
