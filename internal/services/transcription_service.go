package services

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TranscriptionService struct {
	db                  *gorm.DB
	whisperService      *WhisperService
	auditService        *AuditService
	storageService      *StorageService
	emailService        *EmailService
	notificationService *NotificationService
	mediaService        *MediaService
	jobQueue            chan uuid.UUID
}

func NewTranscriptionService(db *gorm.DB, whisperService *WhisperService, auditService *AuditService, storageService *StorageService, emailService *EmailService, notificationService *NotificationService, mediaService *MediaService) *TranscriptionService {
	if mediaService == nil {
		mediaService = NewMediaService()
	}
	svc := &TranscriptionService{
		db:                  db,
		whisperService:      whisperService,
		auditService:        auditService,
		storageService:      storageService,
		emailService:        emailService,
		notificationService: notificationService,
		mediaService:        mediaService,
		jobQueue:            make(chan uuid.UUID, 100),
	}

	for w := 1; w <= 3; w++ {
		go svc.worker(w)
	}

	go svc.pickupPendingJobs()

	return svc
}

func (s *TranscriptionService) pickupPendingJobs() {
	time.Sleep(3 * time.Second)
	var pendingJobs []models.TranscriptionJob
	if err := s.db.Where("status IN ?", []string{string(models.JobQueued), string(models.JobProcessing)}).Find(&pendingJobs).Error; err == nil {
		for _, j := range pendingJobs {
			log.Printf("[WorkerPool] Resuming pending transcription job %s (transcript %s)", j.ID, j.TranscriptID)
			select {
			case s.jobQueue <- j.ID:
			default:
				log.Printf("[WorkerPool] Job queue full for pending job %s", j.ID)
			}
		}
	}
}

func (s *TranscriptionService) Enqueue(accountID uuid.UUID, transcriptID uuid.UUID, fileID uuid.UUID, audioURL string, language string) (*models.TranscriptionJob, error) {
	job := models.TranscriptionJob{
		AccountID:    accountID,
		TranscriptID: transcriptID,
		FileID:       fileID,
		AudioURL:     audioURL,
		Status:       models.JobQueued,
		Language:     language,
	}

	if err := s.db.Create(&job).Error; err != nil {
		return nil, fmt.Errorf("failed to create transcription job: %w", err)
	}

	s.db.Model(&models.Transcript{}).Where("id = ?", transcriptID).Update("status", models.TranscriptProcessing)

	select {
	case s.jobQueue <- job.ID:
		log.Printf("[WorkerPool] Enqueued transcription job %s", job.ID)
	default:
		log.Printf("[WorkerPool] Job queue full, job %s stored in DB for worker pickup", job.ID)
	}

	return &job, nil
}

func (s *TranscriptionService) worker(id int) {
	for jobID := range s.jobQueue {
		s.processJob(jobID)
	}
}

func (s *TranscriptionService) processJob(jobID uuid.UUID) {
	var job models.TranscriptionJob
	if err := s.db.First(&job, "id = ?", jobID).Error; err != nil {
		log.Printf("[Worker] Job %s not found: %v", jobID, err)
		return
	}

	job.Status = models.JobProcessing
	s.db.Save(&job)

	audioURL := job.AudioURL

	// If audioURL is a public media link (YouTube, Vimeo, Google Drive, direct web link), download & convert to WAV first
	if (strings.HasPrefix(audioURL, "http://") || strings.HasPrefix(audioURL, "https://")) && !strings.Contains(audioURL, "storage.googleapis.com") {
		log.Printf("[Worker] Detected public media link for job %s: %s. Initiating media resolution & conversion...", job.ID, audioURL)
		wavPath, err := s.mediaService.DownloadAndConvert(audioURL)
		if err != nil {
			log.Printf("[Worker] Media resolution/download error for job %s: %v", job.ID, err)
			s.handleFailure(&job, fmt.Sprintf("Failed to process media link: %v", err))
			return
		}
		defer func() {
			_ = os.Remove(wavPath)
		}()

		if s.storageService != nil {
			f, openErr := os.Open(wavPath)
			if openErr == nil {
				objectKey := fmt.Sprintf("imports/%s_%s.wav", job.AccountID, job.ID)
				gcsURL, uploadErr := s.storageService.UploadDirectStream(f, objectKey, "audio/wav")
				f.Close()
				if uploadErr == nil {
					log.Printf("[Worker] Media WAV successfully uploaded to GCS: %s", gcsURL)
					audioURL = gcsURL
					s.db.Model(&models.Transcript{}).Where("id = ?", job.TranscriptID).Update("audio_url", gcsURL)
					s.db.Model(&job).Update("audio_url", gcsURL)
				}
			}
		}
	}

	if s.storageService != nil && strings.Contains(audioURL, "storage.googleapis.com") {
		bucket := s.storageService.BucketName()
		prefix := fmt.Sprintf("https://storage.googleapis.com/%s/", bucket)
		var objectKey string
		if strings.HasPrefix(audioURL, prefix) {
			objectKey = strings.TrimPrefix(audioURL, prefix)
		} else {
			parts := strings.Split(audioURL, "/")
			if len(parts) >= 5 {
				objectKey = strings.Join(parts[4:], "/")
			}
		}

		if objectKey != "" {
			signedURL, err := s.storageService.GenerateDownloadSignedURLByKey(objectKey, 2*time.Hour)
			if err != nil {
				log.Printf("[Worker] Failed to generate signed URL for %s: %v", objectKey, err)
			} else {
				log.Printf("[Worker] Generated signed URL for Whisper: %s", objectKey)
				audioURL = signedURL
			}
		}
	}

	log.Printf("[Worker] Submitting job %s to Whisper API for audio: %s", job.ID, job.AudioURL)

	externalID, err := s.whisperService.Submit(audioURL, true, job.Language)
	if err != nil {
		log.Printf("[Worker] Whisper submission error for job %s: %v", job.ID, err)
		s.handleFailure(&job, fmt.Sprintf("Whisper submission error: %v", err))
		return
	}

	job.ExternalID = externalID
	s.db.Save(&job)

	// Poll Whisper API every 3 seconds
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	timeout := time.After(30 * time.Minute)

	consecutiveErrors := 0
	for {
		select {
		case <-timeout:
			s.handleFailure(&job, "Transcription timed out after 30 minutes")
			return
		case <-ticker.C:
			res, err := s.whisperService.GetResult(externalID)
			if err != nil {
				log.Printf("[Worker] Poll error for external ID %s: %v", externalID, err)
				consecutiveErrors++
				if consecutiveErrors >= 30 {
					s.handleFailure(&job, fmt.Sprintf("Polling Whisper failed: %v", err))
					return
				}
				continue
			}
			consecutiveErrors = 0

			status := strings.ToLower(res.Data.Status)
			if status == "" {
				status = strings.ToLower(res.Status)
			}

			if status == "completed" || (res.Success && len(res.Data.Result.Segments) > 0) {
				speakerBanks, duration, wordCount := s.whisperService.MapSegmentsToSpeakerBanks(res.Data.Result.Segments)
				s.handleSuccess(&job, speakerBanks, duration, wordCount)
				return
			} else if status == "failed" || !res.Success || strings.Contains(strings.ToLower(res.Message), "failed") {
				errMsg := res.Message
				if errMsg == "" {
					errMsg = "Whisper transcription failed"
				}
				s.handleFailure(&job, fmt.Sprintf("API reported failure: %s", errMsg))
				return
			}
		}
	}
}

func (s *TranscriptionService) handleSuccess(job *models.TranscriptionJob, speakerBanks models.SpeakerBanks, duration int, wordCount int) {
	now := time.Now().UTC()

	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Transcript{}).Where("id = ?", job.TranscriptID).Updates(map[string]interface{}{
			"status":        models.TranscriptCompleted,
			"speaker_banks": speakerBanks,
			"duration":      duration,
			"word_count":    wordCount,
		}).Error; err != nil {
			return err
		}

		job.Status = models.JobCompleted
		job.CompletedAt = &now
		if err := tx.Save(job).Error; err != nil {
			return err
		}

		hours := float64(duration) / 3600.0
		tx.Model(&models.Account{}).Where("id = ?", job.AccountID).UpdateColumn("hours_processed", gorm.Expr("hours_processed + ?", hours))
		tx.Model(&models.Account{}).Where("id = ?", job.AccountID).UpdateColumn("transcript_count", gorm.Expr("transcript_count + 1"))

		var transcript models.Transcript
		tx.First(&transcript, "id = ?", job.TranscriptID)

		var ownerUser models.User
		if err := tx.First(&ownerUser, "id = ?", transcript.OwnerID).Error; err == nil && ownerUser.Email != "" && s.emailService != nil {
			go s.emailService.SendTranscriptCompletedEmail(ownerUser.Email, ownerUser.Name, transcript.Title, transcript.ID.String(), duration)
		}

		if s.auditService != nil {
			s.auditService.Log(&job.AccountID, "", transcript.OwnerID, "System/Whisper", "transcription_completed", "transcript", transcript.ID.String(), fmt.Sprintf("Completed transcription for '%s' (duration: %ds, words: %d)", transcript.Title, duration, wordCount), "", "")
		}

		return nil
	})

	if err != nil {
		log.Printf("[Worker System Error] Failed to commit success for job %s: %v", job.ID, err)
	} else {
		log.Printf("[Worker] Job %s successfully processed and committed", job.ID)
		var transcript models.Transcript
		s.db.First(&transcript, "id = ?", job.TranscriptID)
		notif := models.Notification{
			AccountID:     &job.AccountID,
			UserID:        &transcript.OwnerID,
			Type:          "processing_complete",
			Title:         "Transcription Completed",
			Message:       fmt.Sprintf("Your transcript '%s' has finished processing.", transcript.Title),
			ActionURL:     fmt.Sprintf("/transcripts/%s", transcript.ID),
			RecipientRole: "user",
			Read:          false,
		}
		if s.notificationService != nil {
			s.notificationService.CreateNotification(&notif)
		} else {
			s.db.Create(&notif)
		}
	}
}

func (s *TranscriptionService) handleFailure(job *models.TranscriptionJob, reason string) {
	job.Status = models.JobFailed
	job.ErrorMessage = reason
	s.db.Save(job)

	s.db.Model(&models.Transcript{}).Where("id = ?", job.TranscriptID).Update("status", models.TranscriptFailed)

	var transcript models.Transcript
	if err := s.db.First(&transcript, "id = ?", job.TranscriptID).Error; err == nil {
		notif := models.Notification{
			AccountID:     &job.AccountID,
			UserID:        &transcript.OwnerID,
			Type:          "processing_failed",
			Title:         "Transcription Notice",
			Message:       fmt.Sprintf("Processing could not be completed for '%s'. Please ensure the recording contains clear speech and try again.", transcript.Title),
			ActionURL:     "/transcripts",
			RecipientRole: "user",
			Read:          false,
		}
		if s.notificationService != nil {
			s.notificationService.CreateNotification(&notif)
		} else {
			s.db.Create(&notif)
		}

		var ownerUser models.User
		if err := s.db.First(&ownerUser, "id = ?", transcript.OwnerID).Error; err == nil && ownerUser.Email != "" && s.emailService != nil {
			go s.emailService.SendTranscriptFailedEmail(ownerUser.Email, ownerUser.Name, transcript.Title, reason)
		}

		if s.auditService != nil {
			s.auditService.Log(&job.AccountID, "", transcript.OwnerID, "System/Whisper", "transcription_failed", "transcript", transcript.ID.String(), fmt.Sprintf("Whisper failure for '%s': %s", transcript.Title, reason), "", "")
		}
	}

	log.Printf("[Worker System Error] Job %s failed: %s", job.ID, reason)
}
