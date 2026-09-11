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
	AccountName      string     `json:"accountName,omitempty"`
	DeletedByID      *uuid.UUID `json:"deletedById,omitempty"`
	DeletedByName    string     `json:"deletedByName,omitempty"`
	DeletedAt        time.Time  `json:"deletedAt"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	OriginalFolderID *uuid.UUID `json:"originalFolderId,omitempty"`
	AdminTrashed     bool       `json:"adminTrashed"`
}

func (s *RecycleBinService) ListRecycleBin(accountID uuid.UUID) ([]RecycleItemResponse, error) {
	var trashed []models.Transcript
	if err := s.db.Where("account_id = ? AND is_trashed = true AND (admin_trashed = false OR admin_trashed IS NULL)", accountID).Order("updated_at DESC").Find(&trashed).Error; err != nil {
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
			AdminTrashed:     t.AdminTrashed,
		})
	}
	return results, nil
}

func (s *RecycleBinService) Restore(accountID uuid.UUID, transcriptID uuid.UUID, actor *models.User) error {
	var transcript models.Transcript
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = true AND (admin_trashed = false OR admin_trashed IS NULL)", transcriptID, accountID).First(&transcript).Error; err != nil {
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
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = true AND (admin_trashed = false OR admin_trashed IS NULL)", transcriptID, accountID).First(&transcript).Error; err != nil {
		return errors.New("trashed transcript not found")
	}

	// Move to admin recycle bin instead of hard deleting
	transcript.AdminTrashed = true
	transcript.DeletedByID = &actor.ID
	transcript.DeletedByName = actor.Name

	if err := s.db.Save(&transcript).Error; err != nil {
		return err
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "transcript_permanently_deleted_by_user", "transcript", transcript.ID.String(), "User permanently deleted transcript from recycle bin (moved to admin recycle bin): "+transcript.Title, "", "")

	return nil
}

func (s *RecycleBinService) ListAdminRecycleBin() ([]RecycleItemResponse, error) {
	var trashed []models.Transcript
	if err := s.db.Where("is_trashed = true AND admin_trashed = true").Order("updated_at DESC").Find(&trashed).Error; err != nil {
		return nil, err
	}

	var accountIDs []uuid.UUID
	for _, t := range trashed {
		accountIDs = append(accountIDs, t.AccountID)
	}

	accountMap := make(map[uuid.UUID]string)
	if len(accountIDs) > 0 {
		var accounts []models.Account
		if err := s.db.Where("id IN ?", accountIDs).Find(&accounts).Error; err == nil {
			for _, acc := range accounts {
				accountMap[acc.ID] = acc.OwnerName
			}
		}
	}

	var results []RecycleItemResponse
	for _, t := range trashed {
		results = append(results, RecycleItemResponse{
			ID:               t.ID,
			TranscriptID:     t.ID,
			TranscriptTitle:  t.Title,
			OwnerID:          t.OwnerID,
			AccountName:      accountMap[t.AccountID],
			DeletedByID:      t.DeletedByID,
			DeletedByName:    t.DeletedByName,
			DeletedAt:        t.UpdatedAt,
			ExpiresAt:        t.ExpiresAt,
			OriginalFolderID: t.FolderID,
			AdminTrashed:     t.AdminTrashed,
		})
	}
	return results, nil
}

func (s *RecycleBinService) AdminRestore(transcriptID uuid.UUID, actor *models.User) error {
	var transcript models.Transcript
	if err := s.db.Where("id = ? AND is_trashed = true AND admin_trashed = true", transcriptID).First(&transcript).Error; err != nil {
		return errors.New("admin trashed transcript not found")
	}

	transcript.IsTrashed = false
	transcript.AdminTrashed = false
	transcript.DeletedByID = nil
	transcript.DeletedByName = ""
	transcript.ExpiresAt = nil

	if err := s.db.Save(&transcript).Error; err != nil {
		return err
	}

	if transcript.FolderID != nil {
		s.db.Model(&models.Folder{}).Where("id = ? AND account_id = ?", *transcript.FolderID, transcript.AccountID).UpdateColumn("transcript_count", gorm.Expr("transcript_count + 1"))
	}

	s.auditService.Log(&transcript.AccountID, "", actor.ID, actor.Name, "transcript_admin_restored", "transcript", transcript.ID.String(), "Admin restored transcript to owner: "+transcript.Title, "", "")

	return nil
}

func (s *RecycleBinService) AdminPermanentDelete(transcriptID uuid.UUID, actor *models.User) error {
	var transcript models.Transcript
	if err := s.db.Where("id = ? AND is_trashed = true AND admin_trashed = true", transcriptID).First(&transcript).Error; err != nil {
		return errors.New("admin trashed transcript not found")
	}

	if err := s.db.Delete(&transcript).Error; err != nil {
		return err
	}

	s.auditService.Log(&transcript.AccountID, "", actor.ID, actor.Name, "transcript_permanently_purged_by_admin", "transcript", transcript.ID.String(), "Admin permanently purged transcript from system: "+transcript.Title, "", "")

	return nil
}
