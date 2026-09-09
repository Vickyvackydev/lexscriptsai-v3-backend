package services

import (
	"errors"
	"fmt"
	"strings"

	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CauseListService struct {
	db           *gorm.DB
	auditService *AuditService
}

func NewCauseListService(db *gorm.DB, auditService *AuditService) *CauseListService {
	return &CauseListService{db: db, auditService: auditService}
}

func (s *CauseListService) ListCauseLists(accountID uuid.UUID, search string) ([]models.CauseList, error) {
	var lists []models.CauseList
	query := s.db.Preload("Items").Where("account_id = ?", accountID)
	if strings.TrimSpace(search) != "" {
		pattern := "%" + strings.ToLower(strings.TrimSpace(search)) + "%"
		query = query.Where("LOWER(name) LIKE ?", pattern)
	}
	if err := query.Order("created_at DESC").Find(&lists).Error; err != nil {
		return nil, err
	}
	return lists, nil
}

func (s *CauseListService) CreateCauseList(accountID uuid.UUID, ownerID uuid.UUID, name string, folderID *uuid.UUID, actor *models.User) (*models.CauseList, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, errors.New("cause list name cannot be empty")
	}

	folderName := ""
	if folderID != nil {
		var folder models.Folder
		if err := s.db.Where("id = ? AND account_id = ?", *folderID, accountID).First(&folder).Error; err == nil {
			folderName = folder.Name
		}
	}

	list := models.CauseList{
		AccountID:  accountID,
		OwnerID:    ownerID,
		Name:       trimmed,
		FolderID:   folderID,
		FolderName: folderName,
	}

	if err := s.db.Create(&list).Error; err != nil {
		return nil, err
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "cause_list_created", "cause_list", list.ID.String(), "Created cause list: "+list.Name, "", "")

	return &list, nil
}

func (s *CauseListService) ListMatters(accountID uuid.UUID, date string, folderID *uuid.UUID, search string) ([]models.CauseListItem, error) {
	var items []models.CauseListItem
	query := s.db.Where("account_id = ?", accountID)

	if date != "" {
		query = query.Where("date = ?", date)
	}

	if folderID != nil {
		query = query.Where("folder_id = ?", *folderID)
	}

	if strings.TrimSpace(search) != "" {
		pattern := "%" + strings.ToLower(strings.TrimSpace(search)) + "%"
		query = query.Where("LOWER(case_number) LIKE ? OR LOWER(parties) LIKE ? OR LOWER(cause_list_name) LIKE ?", pattern, pattern, pattern)
	}

	if err := query.Order("date ASC, time ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

type CreateMatterInput struct {
	Date          string            `json:"date"`
	CaseNumber    string            `json:"caseNumber"`
	Parties       string            `json:"parties"`
	MatterType    models.MatterType `json:"matterType"`
	Time          string            `json:"time"`
	CauseListID   *uuid.UUID        `json:"causeListId"`
	CauseListName string            `json:"causeListName"`
	FolderID      *uuid.UUID        `json:"folderId"`
	Notes         string            `json:"notes"`
}

func (s *CauseListService) CreateMatter(accountID uuid.UUID, input CreateMatterInput, actor *models.User) (*models.CauseListItem, error) {
	if input.CaseNumber == "" || input.Parties == "" || input.Date == "" {
		return nil, errors.New("date, caseNumber, and parties are required fields")
	}

	folderName := ""
	if input.FolderID != nil {
		var folder models.Folder
		if err := s.db.Where("id = ? AND account_id = ?", *input.FolderID, accountID).First(&folder).Error; err == nil {
			folderName = folder.Name
		}
	}

	causeListName := strings.TrimSpace(input.CauseListName)
	causeListID := input.CauseListID

	if causeListID != nil {
		var cl models.CauseList
		if err := s.db.Where("id = ? AND account_id = ?", *causeListID, accountID).First(&cl).Error; err == nil {
			causeListName = cl.Name
		}
	} else if causeListName != "" {
		var existingCl models.CauseList
		if err := s.db.Where("account_id = ? AND name = ?", accountID, causeListName).First(&existingCl).Error; err == nil {
			causeListID = &existingCl.ID
		} else {
			newCl := models.CauseList{
				AccountID:  accountID,
				OwnerID:    actor.ID,
				Name:       causeListName,
				FolderID:   input.FolderID,
				FolderName: folderName,
			}
			if err := s.db.Create(&newCl).Error; err == nil {
				causeListID = &newCl.ID
			}
		}
	}

	matter := models.CauseListItem{
		AccountID:     accountID,
		CauseListID:   causeListID,
		CauseListName: causeListName,
		Date:          input.Date,
		CaseNumber:    input.CaseNumber,
		Parties:       input.Parties,
		MatterType:    input.MatterType,
		Time:          input.Time,
		Status:        models.MatterStatusScheduled,
		FolderID:      input.FolderID,
		FolderName:    folderName,
		Notes:         input.Notes,
	}

	if err := s.db.Create(&matter).Error; err != nil {
		return nil, err
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "matter_created", "matter", matter.ID.String(), "Created matter: "+matter.CaseNumber, "", "")

	return &matter, nil
}

type AdjournMatterInput struct {
	NewDate string `json:"newDate"`
	Reason  string `json:"reason"`
}

func (s *CauseListService) AdjournMatter(accountID uuid.UUID, matterID uuid.UUID, input AdjournMatterInput, actor *models.User) (*models.CauseListItem, error) {
	if input.NewDate == "" {
		return nil, errors.New("adjourned date (newDate) is required")
	}

	var matter models.CauseListItem
	if err := s.db.Where("id = ? AND account_id = ?", matterID, accountID).First(&matter).Error; err != nil {
		return nil, errors.New("matter not found")
	}

	matter.Status = models.MatterStatusAdjourned
	matter.AdjournedToDate = input.NewDate
	matter.AdjournedReason = input.Reason

	if err := s.db.Save(&matter).Error; err != nil {
		return nil, err
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "matter_adjourned", "matter", matter.ID.String(),
		fmt.Sprintf("Adjourned matter %s from %s to %s (Reason: %s)", matter.CaseNumber, matter.Date, input.NewDate, input.Reason), "", "")

	return &matter, nil
}

func (s *CauseListService) DeleteMatter(accountID uuid.UUID, matterID uuid.UUID, actor *models.User) error {
	var matter models.CauseListItem
	if err := s.db.Where("id = ? AND account_id = ?", matterID, accountID).First(&matter).Error; err != nil {
		return errors.New("matter not found")
	}

	if err := s.db.Delete(&matter).Error; err != nil {
		return err
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "matter_deleted", "matter", matter.ID.String(), "Deleted matter: "+matter.CaseNumber, "", "")
	return nil
}
