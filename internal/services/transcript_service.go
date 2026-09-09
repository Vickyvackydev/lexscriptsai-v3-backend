package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TranscriptService struct {
	db               *gorm.DB
	auditService     *AuditService
	transcriptionSvc *TranscriptionService
	storageService   *StorageService
}

func NewTranscriptService(db *gorm.DB, auditService *AuditService, transcriptionSvc *TranscriptionService, storageService *StorageService) *TranscriptService {
	return &TranscriptService{
		db:               db,
		auditService:     auditService,
		transcriptionSvc: transcriptionSvc,
		storageService:   storageService,
	}
}

type ListTranscriptsFilter struct {
	FolderID *uuid.UUID
	Status   string
	Source   string
	Search   string
	Page     int
	PageSize int
}

func (s *TranscriptService) ListTranscripts(accountID uuid.UUID, filter ListTranscriptsFilter) ([]models.Transcript, int64, error) {
	var transcripts []models.Transcript
	var total int64

	query := s.db.Model(&models.Transcript{}).Where("account_id = ? AND is_trashed = false", accountID)

	if filter.FolderID != nil {
		query = query.Where("folder_id = ?", *filter.FolderID)
	}

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	if filter.Source != "" && filter.Source != "all" {
		query = query.Where("source = ?", filter.Source)
	}

	if filter.Search != "" {
		like := "%" + strings.ToLower(filter.Search) + "%"
		query = query.Where("LOWER(title) LIKE ?", like)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&transcripts).Error; err != nil {
		return nil, 0, err
	}

	return transcripts, total, nil
}

func (s *TranscriptService) GetTranscript(accountID uuid.UUID, id uuid.UUID) (*models.Transcript, error) {
	var transcript models.Transcript
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = false", id, accountID).First(&transcript).Error; err != nil {
		return nil, err
	}
	s.SignAudioURL(&transcript)
	return &transcript, nil
}

func (s *TranscriptService) SignAudioURL(t *models.Transcript) {
	if t == nil || t.AudioURL == "" || s.storageService == nil {
		return
	}
	if strings.Contains(t.AudioURL, "storage.googleapis.com") {
		bucket := s.storageService.BucketName()
		prefix := fmt.Sprintf("https://storage.googleapis.com/%s/", bucket)
		var objectKey string
		if strings.HasPrefix(t.AudioURL, prefix) {
			objectKey = strings.TrimPrefix(t.AudioURL, prefix)
		} else {
			parts := strings.Split(t.AudioURL, "/")
			if len(parts) >= 5 {
				objectKey = strings.Join(parts[4:], "/")
			}
		}
		if objectKey != "" {
			if signedURL, err := s.storageService.GenerateDownloadSignedURLByKey(objectKey, 2*time.Hour); err == nil {
				t.AudioURL = signedURL
			}
		}
	}
}

func (s *TranscriptService) RetryTranscript(accountID uuid.UUID, id uuid.UUID, actor *models.User) (*models.Transcript, error) {
	var transcript models.Transcript
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = false", id, accountID).First(&transcript).Error; err != nil {
		return nil, err
	}

	if err := s.db.Model(&transcript).Updates(map[string]interface{}{
		"status": models.TranscriptProcessing,
	}).Error; err != nil {
		return nil, err
	}

	if transcript.AudioURL != "" && s.transcriptionSvc != nil {
		var fileID uuid.UUID
		if transcript.AudioFileID != nil {
			fileID = *transcript.AudioFileID
		}
		_, _ = s.transcriptionSvc.Enqueue(accountID, transcript.ID, fileID, transcript.AudioURL, transcript.Language)
	}

	s.SignAudioURL(&transcript)
	return &transcript, nil
}

func (s *TranscriptService) GetAudioDownloadURL(accountID uuid.UUID, id uuid.UUID) (string, error) {
	var transcript models.Transcript
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = false", id, accountID).First(&transcript).Error; err != nil {
		return "", err
	}
	if transcript.AudioURL == "" {
		return "", errors.New("no audio URL available for this transcript")
	}

	s.SignAudioURL(&transcript)
	return transcript.AudioURL, nil
}

type CreateTranscriptInput struct {
	Title       string     `json:"title"`
	FolderID    *uuid.UUID `json:"folderId"`
	MatterID    *uuid.UUID `json:"matterId"`
	AudioFileID *uuid.UUID `json:"audioFileId"`
	AudioURL    string     `json:"audioUrl"`
	Language    string     `json:"language"`
	Flags       []float64  `json:"flags"`
	Source      string     `json:"source"`
}

func (s *TranscriptService) CreateTranscript(accountID uuid.UUID, ownerID uuid.UUID, input CreateTranscriptInput, actor *models.User) (*models.Transcript, error) {
	if input.Title == "" {
		input.Title = "Untitled Recording - " + time.Now().Format("Jan 02 15:04")
	}

	status := models.TranscriptDraft
	if input.AudioURL != "" || input.AudioFileID != nil {
		status = models.TranscriptProcessing
	}

	source := input.Source
	if source == "" {
		source = "upload"
	}

	transcript := models.Transcript{
		AccountID:    accountID,
		OwnerID:      ownerID,
		FolderID:     input.FolderID,
		MatterID:     input.MatterID,
		AudioFileID:  input.AudioFileID,
		AudioURL:     input.AudioURL,
		Title:        input.Title,
		Status:       status,
		Language:     input.Language,
		Flags:        models.Float64Slice(input.Flags),
		Source:       source,
		SpeakerBanks: models.SpeakerBanks{},
	}

	if err := s.db.Create(&transcript).Error; err != nil {
		return nil, fmt.Errorf("failed to create transcript: %w", err)
	}

	if input.FolderID != nil {
		s.db.Model(&models.Folder{}).Where("id = ? AND account_id = ?", *input.FolderID, accountID).UpdateColumn("transcript_count", gorm.Expr("transcript_count + 1"))
	}

	if input.AudioURL != "" && s.transcriptionSvc != nil {
		fileID := uuid.Nil
		if input.AudioFileID != nil {
			fileID = *input.AudioFileID
		}
		s.transcriptionSvc.Enqueue(accountID, transcript.ID, fileID, input.AudioURL, input.Language)
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "transcript_created", "transcript", transcript.ID.String(), "Created transcript: "+transcript.Title, "", "")

	return &transcript, nil
}

type UpdateTranscriptInput struct {
	Title        *string                  `json:"title"`
	FolderID     *uuid.UUID               `json:"folderId"`
	SpeakerBanks *models.SpeakerBanks     `json:"speakerBanks"`
	Status       *models.TranscriptStatus `json:"status"`
}

func (s *TranscriptService) UpdateTranscript(accountID uuid.UUID, id uuid.UUID, input UpdateTranscriptInput, actor *models.User) (*models.Transcript, error) {
	var transcript models.Transcript
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = false", id, accountID).First(&transcript).Error; err != nil {
		return nil, errors.New("transcript not found")
	}

	if input.Title != nil {
		transcript.Title = *input.Title
	}

	if input.FolderID != nil {
		transcript.FolderID = input.FolderID
	}

	if input.Status != nil {
		transcript.Status = *input.Status
	}

	if input.SpeakerBanks != nil {
		transcript.SpeakerBanks = *input.SpeakerBanks
		count := 0
		for _, sb := range *input.SpeakerBanks {
			count += len(sb.Words)
		}
		transcript.WordCount = count
	}

	if err := s.db.Save(&transcript).Error; err != nil {
		return nil, err
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "transcript_edited", "transcript", transcript.ID.String(), "Saved edits for transcript: "+transcript.Title, "", "")

	return &transcript, nil
}

func (s *TranscriptService) MoveToTrash(accountID uuid.UUID, id uuid.UUID, actor *models.User) error {
	var transcript models.Transcript
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = false", id, accountID).First(&transcript).Error; err != nil {
		return errors.New("transcript not found")
	}

	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour)

	transcript.IsTrashed = true
	transcript.DeletedByID = &actor.ID
	transcript.DeletedByName = actor.Name
	transcript.ExpiresAt = &expiresAt

	if err := s.db.Save(&transcript).Error; err != nil {
		return err
	}

	if transcript.FolderID != nil {
		s.db.Model(&models.Folder{}).Where("id = ? AND account_id = ?", *transcript.FolderID, accountID).UpdateColumn("transcript_count", gorm.Expr("GREATEST(transcript_count - 1, 0)"))
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "transcript_deleted", "transcript", transcript.ID.String(), "Moved transcript to recycle bin: "+transcript.Title, "", "")

	return nil
}
