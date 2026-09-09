package services

import (
	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AuditService struct {
	db *gorm.DB
}

func NewAuditService(db *gorm.DB) *AuditService {
	return &AuditService{db: db}
}

func (s *AuditService) Log(accountID *uuid.UUID, accountName string, actorID uuid.UUID, actorName string, action string, resourceType string, resourceID string, details string, ipAddress string, userAgent string) {
	entry := models.AuditLog{
		AccountID:    accountID,
		AccountName:  accountName,
		ActorID:      actorID,
		ActorName:    actorName,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
		IPAddress:    ipAddress,
		UserAgent:    userAgent,
	}
	go func(e models.AuditLog) {
		s.db.Create(&e)
	}(entry)
}

func (s *AuditService) GetLogs(accountID *uuid.UUID, page int, pageSize int) ([]models.AuditLog, int64, error) {
	var logs []models.AuditLog
	var total int64

	query := s.db.Model(&models.AuditLog{})
	if accountID != nil {
		query = query.Where("account_id = ?", *accountID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

func (s *AuditService) ClearLogs(accountID *uuid.UUID) error {
	if accountID != nil {
		return s.db.Where("account_id = ?", *accountID).Delete(&models.AuditLog{}).Error
	}
	return s.db.Exec("DELETE FROM audit_logs").Error
}
