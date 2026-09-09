package services

import (
	"errors"
	"time"

	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RecycleBinService struct {
	db           *gorm.DB
	auditService *AuditService
}

func NewRecycleBinService(db *gorm.DB, auditService *AuditService) *RecycleBinService {
	return &RecycleBinService{db: db, auditService: auditService}
}

type RecycleItemResponse struct {
	ID               uuid.UUID  `json:"id"`
	TranscriptID     uuid.UUID  `json:"transcriptId"`
	TranscriptTitle  string     `json:"transcriptTitle"`
	OwnerID          uuid.UUID  `json:"ownerId"`
	DeletedByID      *uuid.UUID `json:"deletedById,omitempty"`
	DeletedByName    string     `json:"deletedByName,omitempty"`
	DeletedAt        time.Time  `json:"deletedAt"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	OriginalFolderID *uuid.UUID `json:"originalFolderId,omitempty"`
}

func (s *RecycleBinService) ListRecycleBin(accountID uuid.UUID) ([]RecycleItemResponse, error) {
	var trashed []models.Transcript
	if err := s.db.Where("account_id = ? AND is_trashed = true", accountID).Order("updated_at DESC").Find(&trashed).Error; err != nil {
		return nil, err
	}

	var results []RecycleItemResponse
	for _, t := range trashed {
		results = append(results, RecycleItemResponse{
			ID:               t.ID,
			TranscriptID:     t.ID,
			TranscriptTitle:  t.Title,
			OwnerID:          t.OwnerID,
			DeletedByID:      t.DeletedByID,
			DeletedByName:    t.DeletedByName,
			DeletedAt:        t.UpdatedAt,
			ExpiresAt:        t.ExpiresAt,
			OriginalFolderID: t.FolderID,
		})
	}
	return results, nil
}

func (s *RecycleBinService) Restore(accountID uuid.UUID, transcriptID uuid.UUID, actor *models.User) error {
	var transcript models.Transcript
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = true", transcriptID, accountID).First(&transcript).Error; err != nil {
		return errors.New("trashed transcript not found")
	}

	transcript.IsTrashed = false
	transcript.DeletedByID = nil
	transcript.DeletedByName = ""
	transcript.ExpiresAt = nil

	if err := s.db.Save(&transcript).Error; err != nil {
		return err
	}

	if transcript.FolderID != nil {
		s.db.Model(&models.Folder{}).Where("id = ? AND account_id = ?", *transcript.FolderID, accountID).UpdateColumn("transcript_count", gorm.Expr("transcript_count + 1"))
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "transcript_restored", "transcript", transcript.ID.String(), "Restored transcript from recycle bin: "+transcript.Title, "", "")

	return nil
}

func (s *RecycleBinService) PermanentDelete(accountID uuid.UUID, transcriptID uuid.UUID, actor *models.User) error {
	var transcript models.Transcript
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = true", transcriptID, accountID).First(&transcript).Error; err != nil {
		return errors.New("trashed transcript not found")
	}

	if err := s.db.Delete(&transcript).Error; err != nil {
		return err
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "transcript_permanently_deleted", "transcript", transcript.ID.String(), "Permanently purged transcript: "+transcript.Title, "", "")

	return nil
}
