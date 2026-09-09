package services

import (
	"errors"

	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var DefaultMemberPermissions = models.StringSlice{"view", "edit", "upload", "export", "share", "collaborate", "delete", "shared_files"}

type MemberWithPermissions struct {
	ID                      uuid.UUID               `json:"id"`
	FirstName               string                  `json:"firstName"`
	LastName                string                  `json:"lastName"`
	Name                    string                  `json:"name"`
	Email                   string                  `json:"email"`
	ProfessionalRole        models.ProfessionalRole `json:"role"`
	Status                  models.AccountStatus    `json:"status"`
	Location                string                  `json:"location"`
	Permissions             []string                `json:"permissions"`
	AccessibleTranscriptIDs string                  `json:"accessibleTranscriptIds"`
}

type PermissionService struct {
	db           *gorm.DB
	auditService *AuditService
}

func NewPermissionService(db *gorm.DB, auditService *AuditService) *PermissionService {
	return &PermissionService{db: db, auditService: auditService}
}

func (s *PermissionService) GetDB() *gorm.DB {
	return s.db
}

type UpdatePermissionInput struct {
	Permissions             []string `json:"permissions"`
	AccessibleTranscriptIDs string   `json:"accessibleTranscriptIds"`
}

func (s *PermissionService) ListAccountMembers(accountID uuid.UUID, excludeUserID uuid.UUID) ([]MemberWithPermissions, error) {
	if accountID == uuid.Nil {
		return []MemberWithPermissions{}, nil
	}

	var users []models.User
	query := s.db.Where("account_id = ?", accountID)
	if excludeUserID != uuid.Nil {
		query = query.Where("id != ?", excludeUserID)
	}
	query = query.Where("system_role != ? AND system_role != ?", models.RoleOwner, models.RoleAdmin)

	if err := query.Order("created_at ASC").Find(&users).Error; err != nil {
		return nil, err
	}

	// Fallback if role wasn't tagged as standard sub_account
	if len(users) == 0 {
		var fallbackUsers []models.User
		fbQuery := s.db.Where("account_id = ?", accountID)
		if excludeUserID != uuid.Nil {
			fbQuery = fbQuery.Where("id != ?", excludeUserID)
		}
		if err := fbQuery.Order("created_at ASC").Find(&fallbackUsers).Error; err == nil {
			users = fallbackUsers
		}
	}

	result := make([]MemberWithPermissions, 0, len(users))
	for _, u := range users {
		var perm models.MemberPermission
		perms := []string(DefaultMemberPermissions)
		accessible := "all"

		if err := s.db.Where("user_id = ?", u.ID).First(&perm).Error; err == nil {
			if perm.Permissions != nil {
				perms = []string(perm.Permissions)
			} else {
				perms = []string{}
			}
			accessible = perm.AccessibleTranscriptIDs
		} else {
			// If no permission record exists yet, initialize it
			perm = models.MemberPermission{
				AccountID:               accountID,
				UserID:                  u.ID,
				Permissions:             DefaultMemberPermissions,
				AccessibleTranscriptIDs: "all",
			}
			_ = s.db.Create(&perm).Error
		}

		result = append(result, MemberWithPermissions{
			ID:                      u.ID,
			FirstName:               u.FirstName,
			LastName:                u.LastName,
			Name:                    u.Name,
			Email:                   u.Email,
			ProfessionalRole:        u.ProfessionalRole,
			Status:                  u.Status,
			Location:                u.Location,
			Permissions:             perms,
			AccessibleTranscriptIDs: accessible,
		})
	}

	return result, nil
}

func (s *PermissionService) GetMemberPermission(accountID uuid.UUID, targetUserID uuid.UUID) (*models.MemberPermission, error) {
	var user models.User
	query := s.db.Where("id = ?", targetUserID)
	if accountID != uuid.Nil {
		query = query.Where("account_id = ?", accountID)
	}
	if err := query.First(&user).Error; err != nil {
		return nil, errors.New("member not found in this account")
	}
	if accountID == uuid.Nil && user.AccountID != nil {
		accountID = *user.AccountID
	}

	var perm models.MemberPermission
	if err := s.db.Where("user_id = ?", targetUserID).First(&perm).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			perm = models.MemberPermission{
				AccountID:               accountID,
				UserID:                  targetUserID,
				Permissions:             DefaultMemberPermissions,
				AccessibleTranscriptIDs: "all",
			}
			s.db.Create(&perm)
			return &perm, nil
		}
		return nil, err
	}

	return &perm, nil
}

func (s *PermissionService) UpdateMemberPermission(accountID uuid.UUID, targetUserID uuid.UUID, input UpdatePermissionInput, actor *models.User) (*models.MemberPermission, error) {
	var user models.User
	query := s.db.Where("id = ?", targetUserID)
	if accountID != uuid.Nil {
		query = query.Where("account_id = ?", accountID)
	}
	if err := query.First(&user).Error; err != nil {
		return nil, errors.New("member not found in this account")
	}
	if accountID == uuid.Nil && user.AccountID != nil {
		accountID = *user.AccountID
	}

	var perm models.MemberPermission
	if err := s.db.Where("user_id = ?", targetUserID).First(&perm).Error; err != nil {
		perm = models.MemberPermission{
			AccountID: accountID,
			UserID:    targetUserID,
		}
	}

	if input.Permissions == nil {
		perm.Permissions = models.StringSlice{}
	} else {
		perm.Permissions = models.StringSlice(input.Permissions)
	}
	if input.AccessibleTranscriptIDs != "" {
		perm.AccessibleTranscriptIDs = input.AccessibleTranscriptIDs
	} else {
		perm.AccessibleTranscriptIDs = "all"
	}

	if err := s.db.Save(&perm).Error; err != nil {
		return nil, err
	}

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "permission_updated", "user", targetUserID.String(), "Updated transcript permissions for member "+user.Email, "", "")

	return &perm, nil
}
