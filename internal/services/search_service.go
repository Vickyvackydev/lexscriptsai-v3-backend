package services

import (
	"strings"

	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SearchService struct {
	db *gorm.DB
}

func NewSearchService(db *gorm.DB) *SearchService {
	return &SearchService{db: db}
}

type CategoryResults[T any] struct {
	Items   []T   `json:"items"`
	Total   int64 `json:"total"`
	HasMore bool  `json:"hasMore"`
}

type GlobalSearchResults struct {
	Transcripts CategoryResults[models.Transcript] `json:"transcripts"`
	Folders     CategoryResults[models.Folder]     `json:"folders"`
	CauseLists  CategoryResults[models.CauseList]  `json:"causeLists"`
}

func (s *SearchService) GlobalSearch(accountID uuid.UUID, query string) (*GlobalSearchResults, error) {
	results := &GlobalSearchResults{
		Transcripts: CategoryResults[models.Transcript]{Items: []models.Transcript{}},
		Folders:     CategoryResults[models.Folder]{Items: []models.Folder{}},
		CauseLists:  CategoryResults[models.CauseList]{Items: []models.CauseList{}},
	}

	q := strings.TrimSpace(query)
	if q == "" {
		return results, nil
	}

	pattern := "%" + strings.ToLower(q) + "%"
	limit := 6

	// Transcripts
	tQuery := s.db.Model(&models.Transcript{}).Where("account_id = ? AND is_trashed = false AND LOWER(title) LIKE ?", accountID, pattern)
	tQuery.Count(&results.Transcripts.Total)
	tQuery.Order("created_at DESC").Limit(limit).Find(&results.Transcripts.Items)
	results.Transcripts.HasMore = results.Transcripts.Total > int64(limit)

	// Folders
	fQuery := s.db.Model(&models.Folder{}).Where("account_id = ? AND is_trashed = false AND LOWER(name) LIKE ?", accountID, pattern)
	fQuery.Count(&results.Folders.Total)
	fQuery.Order("created_at DESC").Limit(limit).Find(&results.Folders.Items)
	results.Folders.HasMore = results.Folders.Total > int64(limit)

	if len(results.Folders.Items) > 0 {
		var folderIDs []uuid.UUID
		for _, f := range results.Folders.Items {
			folderIDs = append(folderIDs, f.ID)
		}
		type countResult struct {
			FolderID uuid.UUID `gorm:"column:folder_id"`
			Count    int       `gorm:"column:count"`
		}
		var counts []countResult
		s.db.Model(&models.Transcript{}).
			Select("folder_id, count(*) as count").
			Where("folder_id IN ? AND (is_trashed = false OR is_trashed IS NULL)", folderIDs).
			Group("folder_id").
			Scan(&counts)

		countMap := make(map[uuid.UUID]int)
		for _, c := range counts {
			countMap[c.FolderID] = c.Count
		}
		for i := range results.Folders.Items {
			results.Folders.Items[i].TranscriptCount = countMap[results.Folders.Items[i].ID]
		}
	}

	// Cause Lists
	cQuery := s.db.Model(&models.CauseList{}).Where("account_id = ? AND LOWER(name) LIKE ?", accountID, pattern)
	cQuery.Count(&results.CauseLists.Total)
	cQuery.Order("created_at DESC").Limit(limit).Find(&results.CauseLists.Items)
	results.CauseLists.HasMore = results.CauseLists.Total > int64(limit)

	return results, nil
}

type AdminUserItem struct {
	models.User
	AccountName string `json:"accountName,omitempty"`
}

type AdminSearchResults struct {
	Accounts CategoryResults[models.Account] `json:"accounts"`
	Users    CategoryResults[AdminUserItem]  `json:"users"`
}

func (s *SearchService) AdminSearch(query string) (*AdminSearchResults, error) {
	results := &AdminSearchResults{
		Accounts: CategoryResults[models.Account]{Items: []models.Account{}},
		Users:    CategoryResults[AdminUserItem]{Items: []AdminUserItem{}},
	}

	q := strings.TrimSpace(query)
	if q == "" {
		return results, nil
	}

	pattern := "%" + strings.ToLower(q) + "%"
	limit := 10

	// Accounts
	aQuery := s.db.Model(&models.Account{}).Where("LOWER(owner_name) LIKE ? OR LOWER(owner_email) LIKE ? OR LOWER(location) LIKE ?", pattern, pattern, pattern)
	aQuery.Count(&results.Accounts.Total)
	aQuery.Order("created_at DESC").Limit(limit).Find(&results.Accounts.Items)
	results.Accounts.HasMore = results.Accounts.Total > int64(limit)

	// Users
	var users []models.User
	uQuery := s.db.Model(&models.User{}).Where("LOWER(name) LIKE ? OR LOWER(email) LIKE ? OR LOWER(location) LIKE ?", pattern, pattern, pattern)
	uQuery.Count(&results.Users.Total)
	uQuery.Order("created_at DESC").Limit(limit).Find(&users)
	results.Users.HasMore = results.Users.Total > int64(limit)

	// Populate AccountName
	accountIDs := make([]uuid.UUID, 0)
	for _, u := range users {
		if u.AccountID != nil {
			accountIDs = append(accountIDs, *u.AccountID)
		}
	}

	accountMap := make(map[uuid.UUID]string)
	if len(accountIDs) > 0 {
		var accs []models.Account
		s.db.Select("id, owner_name").Where("id IN ?", accountIDs).Find(&accs)
		for _, acc := range accs {
			accountMap[acc.ID] = acc.OwnerName
		}
	}

	for _, u := range users {
		accName := ""
		if u.AccountID != nil {
			accName = accountMap[*u.AccountID]
		}
		results.Users.Items = append(results.Users.Items, AdminUserItem{
			User:        u,
			AccountName: accName,
		})
	}

	return results, nil
}
