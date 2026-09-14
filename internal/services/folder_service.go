package services

import (
	"errors"
	"strings"

	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FolderService struct {
	db           *gorm.DB
	auditService *AuditService
}

func NewFolderService(db *gorm.DB, auditService *AuditService) *FolderService {
	return &FolderService{db: db, auditService: auditService}
}

func (s *FolderService) ListFolders(accountID uuid.UUID, search string) ([]models.Folder, error) {
	var folders []models.Folder
	query := s.db.Where("account_id = ? AND is_trashed = false", accountID)
	if strings.TrimSpace(search) != "" {
		pattern := "%" + strings.ToLower(strings.TrimSpace(search)) + "%"
		query = query.Where("LOWER(name) LIKE ?", pattern)
	}
	if err := query.Order("created_at DESC").Find(&folders).Error; err != nil {
		return nil, err
	}

	type countResult struct {
		FolderID uuid.UUID
		Count    int
	}
	var counts []countResult
	s.db.Model(&models.Transcript{}).
		Select("folder_id, count(*) as count").
		Where("account_id = ? AND is_trashed = false AND folder_id IS NOT NULL", accountID).
		Group("folder_id").
		Scan(&counts)

	countMap := make(map[uuid.UUID]int)
	for _, c := range counts {
		countMap[c.FolderID] = c.Count
	}

	ownerIDs := make([]uuid.UUID, 0, len(folders))
	for _, f := range folders {
		if f.OwnerID != uuid.Nil {
			ownerIDs = append(ownerIDs, f.OwnerID)
		}
	}
	userMap := make(map[uuid.UUID]string)
	if len(ownerIDs) > 0 {
		var users []models.User
		s.db.Select("id, name, first_name, last_name, email").Where("id IN ?", ownerIDs).Find(&users)
		for _, u := range users {
			n := strings.TrimSpace(u.Name)
			if n == "" {
				n = strings.TrimSpace(u.FirstName + " " + u.LastName)
			}
			if n == "" {
				n = u.Email
			}
			userMap[u.ID] = n
		}
	}

	for i := range folders {
		folders[i].TranscriptCount = countMap[folders[i].ID]
		if name, ok := userMap[folders[i].OwnerID]; ok && name != "" {
			folders[i].OwnerName = name
		}
	}

	return folders, nil
}

func (s *FolderService) CreateFolder(accountID uuid.UUID, ownerID uuid.UUID, name string, actor *models.User) (*models.Folder, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, errors.New("folder name cannot be empty")
	}

	ownerName := ""
	if actor != nil {
		ownerName = actor.Name
	}

	folder := models.Folder{
		AccountID:       accountID,
		OwnerID:         ownerID,
		OwnerName:       ownerName,
		Name:            trimmed,
		TranscriptCount: 0,
		IsTrashed:       false,
	}

	if err := s.db.Create(&folder).Error; err != nil {
		return nil, err
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "folder_created", "folder", folder.ID.String(), "Created folder: "+folder.Name, "", "")

	return &folder, nil
}

func (s *FolderService) RenameFolder(accountID uuid.UUID, folderID uuid.UUID, newName string, actor *models.User) (*models.Folder, error) {
	trimmed := strings.TrimSpace(newName)
	if trimmed == "" {
		return nil, errors.New("folder name cannot be empty")
	}

	var folder models.Folder
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = false", folderID, accountID).First(&folder).Error; err != nil {
		return nil, errors.New("folder not found")
	}

	oldName := folder.Name
	folder.Name = trimmed
	if err := s.db.Save(&folder).Error; err != nil {
		return nil, err
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "folder_renamed", "folder", folder.ID.String(), "Renamed folder from '"+oldName+"' to '"+trimmed+"'", "", "")

	return &folder, nil
}

func (s *FolderService) DeleteFolder(accountID uuid.UUID, folderID uuid.UUID, actor *models.User) error {
	var folder models.Folder
	if err := s.db.Where("id = ? AND account_id = ? AND is_trashed = false", folderID, accountID).First(&folder).Error; err != nil {
		return errors.New("folder not found")
	}

	folder.IsTrashed = true
	if err := s.db.Save(&folder).Error; err != nil {
		return err
	}

	s.db.Model(&models.Transcript{}).Where("folder_id = ?", folderID).Update("folder_id", nil)

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "folder_deleted", "folder", folder.ID.String(), "Deleted folder: "+folder.Name, "", "")

	return nil
}
