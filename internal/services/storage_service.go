package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lexscriptsai-v3-backend/internal/config"
	"lexscriptsai-v3-backend/internal/models"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StorageService struct {
	client  *storage.Client
	bucket  string
	email   string
	baseURL string
	db      *gorm.DB
}

func NewStorageService(cfg *config.Config, db *gorm.DB) (*StorageService, error) {
	ctx := context.Background()

	if cfg.GoogleAppCredentials != "" {
		if _, statErr := os.Stat(cfg.GoogleAppCredentials); statErr == nil {
			os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", cfg.GoogleAppCredentials)
		}
	}

	client, err := storage.NewClient(ctx)
	if err != nil {
		fmt.Printf("[Storage] GCS client initialization notice: %v (GCS operations will use local uploads if credentials are not provided)\n", err)
	}

	email := os.Getenv("GCS_SERVICE_ACCOUNT_EMAIL")
	if email == "" && cfg.GoogleAppCredentials != "" {
		if data, err := os.ReadFile(cfg.GoogleAppCredentials); err == nil {
			var creds struct {
				ClientEmail string `json:"client_email"`
			}
			if err := json.Unmarshal(data, &creds); err == nil {
				email = creds.ClientEmail
			}
		}
	}

	return &StorageService{
		client:  client,
		bucket:  cfg.GCSBucketName,
		email:   email,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		db:      db,
	}, nil
}

const (
	MaxAudioSizeBytes = int64(2) * 1024 * 1024 * 1024 // 2GB
)

var allowedAudioMIMEs = map[string]bool{
	"audio/mpeg":       true,
	"audio/mp3":        true,
	"audio/wav":        true,
	"audio/x-wav":      true,
	"audio/m4a":        true,
	"audio/x-m4a":      true,
	"audio/aac":        true,
	"audio/ogg":        true,
	"audio/webm":       true,
	"video/mp4":        true,
	"video/quicktime":  true,
}

type SignedUploadURLResponse struct {
	FileID    uuid.UUID `json:"fileId"`
	UploadURL string    `json:"uploadUrl"`
	ObjectKey string    `json:"objectKey"`
	ExpiresIn int64     `json:"expiresIn"`
}

func (s *StorageService) GenerateUploadSignedURL(accountID uuid.UUID, ownerID uuid.UUID, filename string, contentType string, sizeBytes int64) (*SignedUploadURLResponse, error) {
	if sizeBytes > MaxAudioSizeBytes {
		return nil, fmt.Errorf("file exceeds maximum allowed size of 2GB (requested: %d bytes)", sizeBytes)
	}

	cTypeClean := strings.ToLower(strings.TrimSpace(contentType))
	if !allowedAudioMIMEs[cTypeClean] && !strings.HasPrefix(cTypeClean, "audio/") {
		return nil, fmt.Errorf("unsupported file content type '%s'. Only court-grade audio/video formats are permitted", contentType)
	}

	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".mp3"
	}
	cleanExt := strings.ToLower(ext)

	objectKey := fmt.Sprintf("audio/%s/%d-%s%s", accountID.String(), time.Now().Unix(), uuid.New().String(), cleanExt)

	fileRecord := models.File{
		AccountID:        accountID,
		OwnerID:          ownerID,
		ObjectKey:        objectKey,
		OriginalFilename: filepath.Base(filename),
		ContentType:      contentType,
		SizeBytes:        sizeBytes,
		Status:           "pending",
	}

	if err := s.db.Create(&fileRecord).Error; err != nil {
		return nil, fmt.Errorf("failed to register file entity: %w", err)
	}

	uploadURL := ""
	if s.client != nil && s.bucket != "" {
		opts := &storage.SignedURLOptions{
			Scheme:      storage.SigningSchemeV4,
			Method:      "PUT",
			Expires:     time.Now().Add(15 * time.Minute),
			ContentType: contentType,
		}
		if s.email != "" {
			opts.GoogleAccessID = s.email
		}
		url, err := s.client.Bucket(s.bucket).SignedURL(objectKey, opts)
		if err == nil {
			uploadURL = url
		}
	}

	if uploadURL == "" {
		uploadURL = fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucket, objectKey)
	}

	return &SignedUploadURLResponse{
		FileID:    fileRecord.ID,
		UploadURL: uploadURL,
		ObjectKey: objectKey,
		ExpiresIn: 900,
	}, nil
}

func (s *StorageService) GenerateDownloadSignedURL(file *models.File, duration time.Duration) (string, error) {
	if file == nil {
		return "", errors.New("file cannot be nil")
	}

	if s.client != nil && s.bucket != "" {
		opts := &storage.SignedURLOptions{
			Scheme:  storage.SigningSchemeV4,
			Method:  "GET",
			Expires: time.Now().Add(duration),
		}
		if s.email != "" {
			opts.GoogleAccessID = s.email
		}
		return s.client.Bucket(s.bucket).SignedURL(file.ObjectKey, opts)
	}

	return fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucket, file.ObjectKey), nil
}

func (s *StorageService) BucketName() string {
	return s.bucket
}

func (s *StorageService) GenerateDownloadSignedURLByKey(objectKey string, duration time.Duration) (string, error) {
	if objectKey == "" {
		return "", errors.New("objectKey cannot be empty")
	}

	if s.client != nil && s.bucket != "" {
		opts := &storage.SignedURLOptions{
			Scheme:  storage.SigningSchemeV4,
			Method:  "GET",
			Expires: time.Now().Add(duration),
		}
		if s.email != "" {
			opts.GoogleAccessID = s.email
		}
		return s.client.Bucket(s.bucket).SignedURL(objectKey, opts)
	}

	return fmt.Sprintf("%s/uploads/%s", s.baseURL, filepath.Base(objectKey)), nil
}

func (s *StorageService) UploadMultipart(ctx context.Context, fileHeader *multipart.FileHeader, accountID uuid.UUID, ownerID uuid.UUID) (*models.File, string, error) {
	if fileHeader.Size > MaxAudioSizeBytes {
		return nil, "", fmt.Errorf("file exceeds maximum size limit of 2GB")
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	objectKey := fmt.Sprintf("audio/%s/%d-%s%s", accountID.String(), time.Now().Unix(), uuid.New().String(), ext)

	fileRecord := models.File{
		AccountID:        accountID,
		OwnerID:          ownerID,
		ObjectKey:        objectKey,
		OriginalFilename: filepath.Base(fileHeader.Filename),
		ContentType:      fileHeader.Header.Get("Content-Type"),
		SizeBytes:        fileHeader.Size,
		Status:           "uploaded",
	}

	var publicURL string
	uploadedToGCS := false

	if s.client != nil && s.bucket != "" {
		src, err := fileHeader.Open()
		if err == nil {
			wc := s.client.Bucket(s.bucket).Object(objectKey).NewWriter(ctx)
			if _, errCopy := io.Copy(wc, src); errCopy == nil {
				if errClose := wc.Close(); errClose == nil {
					publicURL = fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucket, objectKey)
					uploadedToGCS = true
				} else {
					fmt.Printf("[Storage Notice] GCS write/close failed (%v). Falling back to local file storage.\n", errClose)
				}
			} else {
				_ = wc.Close()
				fmt.Printf("[Storage Notice] GCS copy failed (%v). Falling back to local file storage.\n", errCopy)
			}
			_ = src.Close()
		}
	}

	if !uploadedToGCS {
		_ = os.MkdirAll("uploads", 0755)
		localFilename := filepath.Base(objectKey)
		localPath := filepath.Join("uploads", localFilename)

		out, err := os.Create(localPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create local file: %w", err)
		}
		defer out.Close()

		src, err := fileHeader.Open()
		if err != nil {
			return nil, "", fmt.Errorf("failed to read upload file: %w", err)
		}
		defer src.Close()

		if _, err = io.Copy(out, src); err != nil {
			return nil, "", fmt.Errorf("failed to write local file: %w", err)
		}
		publicURL = fmt.Sprintf("%s/uploads/%s", s.baseURL, localFilename)
	}

	if err := s.db.Create(&fileRecord).Error; err != nil {
		return nil, "", err
	}

	return &fileRecord, publicURL, nil
}

func (s *StorageService) UploadDirectStream(r io.Reader, objectKey string, contentType string) (string, error) {
	ctx := context.Background()
	var publicURL string
	uploadedToGCS := false

	if s.client != nil && s.bucket != "" {
		wc := s.client.Bucket(s.bucket).Object(objectKey).NewWriter(ctx)
		if contentType != "" {
			wc.ContentType = contentType
		}
		if _, errCopy := io.Copy(wc, r); errCopy == nil {
			if errClose := wc.Close(); errClose == nil {
				publicURL = fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucket, objectKey)
				uploadedToGCS = true
			}
		}
	}

	if !uploadedToGCS {
		_ = os.MkdirAll("uploads", 0755)
		localFilename := filepath.Base(objectKey)
		localPath := filepath.Join("uploads", localFilename)

		out, err := os.Create(localPath)
		if err != nil {
			return "", fmt.Errorf("failed to create local file: %w", err)
		}
		defer out.Close()

		if _, err = io.Copy(out, r); err != nil {
			return "", fmt.Errorf("failed to write local file: %w", err)
		}
		publicURL = fmt.Sprintf("%s/uploads/%s", s.baseURL, localFilename)
	}

	return publicURL, nil
}
