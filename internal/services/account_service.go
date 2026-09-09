package services

import (
	"errors"
	"fmt"
	"strings"

	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AccountService struct {
	db           *gorm.DB
	auditService *AuditService
	emailService *EmailService
	frontendURL  string
}

func NewAccountService(db *gorm.DB, auditService *AuditService, emailService *EmailService, frontendURL string) *AccountService {
	return &AccountService{
		db:           db,
		auditService: auditService,
		emailService: emailService,
		frontendURL:  frontendURL,
	}
}

type CreateAccountInput struct {
	OwnerFirstName string             `json:"ownerFirstName"`
	OwnerLastName  string             `json:"ownerLastName"`
	OwnerEmail     string             `json:"ownerEmail"`
	Password       string             `json:"password"`
	LocationID     *uuid.UUID         `json:"locationId"`
	Location       string             `json:"location"`
	SubAccounts    []CreateSubAccount `json:"subAccounts"`
}

type CreateSubAccount struct {
	FirstName string                  `json:"firstName"`
	LastName  string                  `json:"lastName"`
	Email     string                  `json:"email"`
	Password  string                  `json:"password"`
	Role      models.ProfessionalRole `json:"role"`
	Location  string                  `json:"location"`
}

func (s *AccountService) CreateAccount(input CreateAccountInput, actor *models.User) (*models.Account, error) {
	if input.OwnerEmail == "" {
		return nil, errors.New("owner email is required")
	}

	if strings.TrimSpace(input.Location) == "" {
		return nil, errors.New("location is compulsory for the account owner")
	}

	for i, sub := range input.SubAccounts {
		role := strings.ToLower(string(sub.Role))
		if role != string(models.RoleJudge) && role != string(models.RoleCourtReporter) && role != string(models.RoleScopist) {
			return nil, fmt.Errorf("invalid professional role '%s'. Allowed: judge, court_reporter, scopist", sub.Role)
		}
		if strings.TrimSpace(sub.Location) == "" {
			subIdentifier := sub.Email
			if sub.FirstName != "" || sub.LastName != "" {
				subIdentifier = strings.TrimSpace(sub.FirstName + " " + sub.LastName)
			}
			return nil, fmt.Errorf("location is compulsory for sub-account #%d (%s)", i+1, subIdentifier)
		}
	}

	var existingCount int64
	s.db.Model(&models.User{}).Where("LOWER(email) = LOWER(?)", input.OwnerEmail).Count(&existingCount)
	if existingCount > 0 {
		return nil, errors.New("a user with this email address already exists")
	}

	fullName := strings.TrimSpace(input.OwnerFirstName + " " + input.OwnerLastName)
	if fullName == "" {
		fullName = input.OwnerEmail
	}

	initials := ""
	if len(input.OwnerFirstName) > 0 {
		initials += strings.ToUpper(string(input.OwnerFirstName[0]))
	}
	if len(input.OwnerLastName) > 0 {
		initials += strings.ToUpper(string(input.OwnerLastName[0]))
	}
	if initials == "" {
		initials = "AC"
	}

	rawPw := input.Password
	if rawPw == "" {
		rawPw = "DefaultPassword2026!"
	}
	pwHash, err := bcrypt.GenerateFromPassword([]byte(rawPw), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	var account models.Account

	err = s.db.Transaction(func(tx *gorm.DB) error {
		account = models.Account{
			OwnerFirstName: input.OwnerFirstName,
			OwnerLastName:  input.OwnerLastName,
			OwnerName:      fullName,
			OwnerEmail:     input.OwnerEmail,
			OwnerRole:      "Account Owner",
			LocationID:     input.LocationID,
			Location:       input.Location,
			Status:         models.StatusActive,
			UserCount:      len(input.SubAccounts),
		}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}

		ownerUser := models.User{
			AccountID:      &account.ID,
			FirstName:      input.OwnerFirstName,
			LastName:       input.OwnerLastName,
			Name:           fullName,
			Email:          input.OwnerEmail,
			PasswordHash:   string(pwHash),
			SystemRole:     models.RoleOwner,
			LocationID:     input.LocationID,
			Location:       input.Location,
			Status:         models.StatusActive,
			AvatarInitials: initials,
		}
		if err := tx.Create(&ownerUser).Error; err != nil {
			return err
		}

		for _, sub := range input.SubAccounts {
			subFullName := strings.TrimSpace(sub.FirstName + " " + sub.LastName)
			if subFullName == "" {
				subFullName = sub.Email
			}

			subInitials := ""
			if len(sub.FirstName) > 0 {
				subInitials += strings.ToUpper(string(sub.FirstName[0]))
			}
			if len(sub.LastName) > 0 {
				subInitials += strings.ToUpper(string(sub.LastName[0]))
			}
			if subInitials == "" {
				subInitials = "US"
			}

			subPw := sub.Password
			if subPw == "" {
				subPw = "SubPassword2026!"
			}
			subPwHash, _ := bcrypt.GenerateFromPassword([]byte(subPw), bcrypt.DefaultCost)

			subUser := models.User{
				AccountID:        &account.ID,
				FirstName:        sub.FirstName,
				LastName:         sub.LastName,
				Name:             subFullName,
				Email:            sub.Email,
				PasswordHash:     string(subPwHash),
				SystemRole:       models.RoleSubAccount,
				ProfessionalRole: sub.Role,
				Location:         sub.Location,
				Status:           models.StatusActive,
				AvatarInitials:   subInitials,
			}
			if err := tx.Create(&subUser).Error; err != nil {
				return err
			}

			subPerm := models.MemberPermission{
				AccountID:               account.ID,
				UserID:                  subUser.ID,
				Permissions:             DefaultMemberPermissions,
				AccessibleTranscriptIDs: "all",
			}
			_ = tx.Create(&subPerm).Error

			subNotif := models.Notification{
				AccountID:     &account.ID,
				UserID:        &subUser.ID,
				UserEmail:     subUser.Email,
				Type:          "system",
				Title:         "Welcome to LexScripts AI",
				Message:       "Your account is active. Explore your transcripts, cause lists, and legal workflows.",
				ActionURL:     "/transcripts",
				RecipientRole: "user",
				Read:          false,
			}
			_ = tx.Create(&subNotif).Error

			go s.emailService.SendWelcomeEmail(sub.Email, subFullName, subPw, string(sub.Role), s.frontendURL+"/login")
		}

		ownerWelcomeNotif := models.Notification{
			AccountID:     &account.ID,
			UserID:        &ownerUser.ID,
			UserEmail:     ownerUser.Email,
			Type:          "system",
			Title:         "Welcome to LexScripts AI",
			Message:       "Your organization account is ready. Configure member permissions, upload transcripts, and manage cause lists.",
			ActionURL:     "/members",
			RecipientRole: "user",
			Read:          false,
		}
		_ = tx.Create(&ownerWelcomeNotif).Error

		return nil
	})

	if err != nil {
		return nil, err
	}

	go s.emailService.SendWelcomeEmail(input.OwnerEmail, fullName, rawPw, "Account Owner", s.frontendURL+"/login")

	actorID := uuid.Nil
	actorName := "System"
	if actor != nil {
		actorID = actor.ID
		actorName = actor.Name
	}
	s.auditService.Log(&account.ID, "", actorID, actorName, "account_created", "account", account.ID.String(), "Created account: "+account.OwnerName, "", "")

	s.db.Preload("Users").Where("id = ?", account.ID).First(&account)
	return &account, nil
}

func (s *AccountService) AddSubAccount(accountID uuid.UUID, input CreateSubAccount, actor *models.User) (*models.User, error) {
	var account models.Account
	if err := s.db.Where("id = ?", accountID).First(&account).Error; err != nil {
		return nil, errors.New("account not found")
	}

	if strings.TrimSpace(input.Location) == "" {
		return nil, errors.New("location is compulsory for the sub-account")
	}

	var existingCount int64
	s.db.Model(&models.User{}).Where("LOWER(email) = LOWER(?)", input.Email).Count(&existingCount)
	if existingCount > 0 {
		return nil, errors.New("a user with this email address already exists")
	}

	fullName := strings.TrimSpace(input.FirstName + " " + input.LastName)
	if fullName == "" {
		fullName = input.Email
	}

	initials := ""
	if len(input.FirstName) > 0 {
		initials += strings.ToUpper(string(input.FirstName[0]))
	}
	if len(input.LastName) > 0 {
		initials += strings.ToUpper(string(input.LastName[0]))
	}
	if initials == "" {
		initials = "US"
	}

	rawPw := input.Password
	if rawPw == "" {
		rawPw = "DefaultPassword2026!"
	}
	pwHash, err := bcrypt.GenerateFromPassword([]byte(rawPw), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := models.User{
		AccountID:        &accountID,
		FirstName:        input.FirstName,
		LastName:         input.LastName,
		Name:             fullName,
		Email:            input.Email,
		PasswordHash:     string(pwHash),
		SystemRole:       models.RoleSubAccount,
		ProfessionalRole: input.Role,
		Location:         input.Location,
		Status:           models.StatusActive,
		AvatarInitials:   initials,
	}

	if err := s.db.Create(&user).Error; err != nil {
		return nil, err
	}

	s.db.Model(&account).Update("user_count", gorm.Expr("user_count + 1"))

	// In-app notifications for owner and newly added sub-account
	ownerNotif := models.Notification{
		AccountID:     &accountID,
		Type:          "account",
		Title:         "Member Added",
		Message:       fmt.Sprintf("%s has been added to your organization as %s.", user.Name, user.ProfessionalRole),
		ActionURL:     "/members",
		RecipientRole: "user",
		Read:          false,
	}
	_ = s.db.Create(&ownerNotif).Error

	subNotif := models.Notification{
		AccountID:     &accountID,
		UserID:        &user.ID,
		UserEmail:     user.Email,
		Type:          "system",
		Title:         "Welcome to LexScripts AI",
		Message:       "Your account is active. Explore your transcripts, cause lists, and legal workflows.",
		ActionURL:     "/transcripts",
		RecipientRole: "user",
		Read:          false,
	}
	_ = s.db.Create(&subNotif).Error

	go s.emailService.SendWelcomeEmail(user.Email, user.Name, rawPw, string(user.ProfessionalRole), s.frontendURL+"/login")

	actorID := uuid.Nil
	actorName := "System"
	if actor != nil {
		actorID = actor.ID
		actorName = actor.Name
	}
	s.auditService.Log(&accountID, "", actorID, actorName, "user_added", "user", user.ID.String(), "Added user: "+user.Name, "", "")

	return &user, nil
}

func (s *AccountService) GetAccount(id uuid.UUID) (*models.Account, error) {
	var account models.Account
	if err := s.db.Preload("Users").Where("id = ?", id).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func (s *AccountService) ListAccounts(page int, pageSize int, search string) ([]models.Account, int64, error) {
	var accounts []models.Account
	var total int64

	query := s.db.Model(&models.Account{})
	if strings.TrimSpace(search) != "" {
		pattern := "%" + strings.ToLower(strings.TrimSpace(search)) + "%"
		query = query.Where("LOWER(owner_name) LIKE ? OR LOWER(owner_email) LIKE ? OR LOWER(location) LIKE ?", pattern, pattern, pattern)
	}

	query.Count(&total)

	offset := (page - 1) * pageSize
	if err := query.Preload("Users").Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&accounts).Error; err != nil {
		return nil, 0, err
	}

	return accounts, total, nil
}

func (s *AccountService) UpdateUserStatus(userID uuid.UUID, status models.AccountStatus, actor *models.User) error {
	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		return errors.New("user not found")
	}

	user.Status = status
	if err := s.db.Save(&user).Error; err != nil {
		return err
	}

	s.auditService.Log(user.AccountID, "", actor.ID, actor.Name, "user_status_updated", "user", user.ID.String(), fmt.Sprintf("Updated user status to %s", status), "", "")
	return nil
}

func (s *AccountService) UpdateAccountStatus(accountID uuid.UUID, status models.AccountStatus, actor *models.User) error {
	var account models.Account
	if err := s.db.Where("id = ?", accountID).First(&account).Error; err != nil {
		return errors.New("account not found")
	}

	account.Status = status
	if err := s.db.Save(&account).Error; err != nil {
		return err
	}

	s.db.Model(&models.User{}).Where("account_id = ?", accountID).Update("status", status)

	s.auditService.Log(&accountID, "", actor.ID, actor.Name, "account_status_updated", "account", account.ID.String(), fmt.Sprintf("Updated account status to %s", status), "", "")
	return nil
}

type UpdateAccountInput struct {
	OwnerFirstName string `json:"ownerFirstName"`
	OwnerLastName  string `json:"ownerLastName"`
	OwnerEmail     string `json:"ownerEmail"`
	Location       string `json:"location"`
	OwnerRole      string `json:"ownerRole"`
}

func (s *AccountService) UpdateAccount(accountID uuid.UUID, input UpdateAccountInput, actor *models.User) (*models.Account, error) {
	var account models.Account
	if err := s.db.Where("id = ?", accountID).First(&account).Error; err != nil {
		return nil, errors.New("account not found")
	}

	if strings.TrimSpace(input.OwnerEmail) == "" {
		return nil, errors.New("email is required")
	}
	if strings.TrimSpace(input.Location) == "" {
		return nil, errors.New("location is compulsory")
	}

	oldEmail := account.OwnerEmail

	if !strings.EqualFold(input.OwnerEmail, oldEmail) {
		var existingCount int64
		s.db.Model(&models.User{}).Where("LOWER(email) = LOWER(?) AND account_id != ?", input.OwnerEmail, accountID).Count(&existingCount)
		if existingCount > 0 {
			return nil, errors.New("email address is already in use by another user")
		}
	}

	fullName := strings.TrimSpace(input.OwnerFirstName + " " + input.OwnerLastName)
	if fullName == "" {
		fullName = input.OwnerEmail
	}

	account.OwnerFirstName = input.OwnerFirstName
	account.OwnerLastName = input.OwnerLastName
	account.OwnerName = fullName
	account.OwnerEmail = input.OwnerEmail
	account.Location = input.Location
	if input.OwnerRole != "" {
		account.OwnerRole = input.OwnerRole
	}

	if err := s.db.Save(&account).Error; err != nil {
		return nil, err
	}

	s.db.Model(&models.User{}).Where("account_id = ? AND system_role = ?", accountID, models.RoleOwner).Updates(map[string]interface{}{
		"first_name": input.OwnerFirstName,
		"last_name":  input.OwnerLastName,
		"name":       fullName,
		"email":      input.OwnerEmail,
		"location":   input.Location,
	})

	actorID := uuid.Nil
	actorName := "System"
	if actor != nil {
		actorID = actor.ID
		actorName = actor.Name
	}
	s.auditService.Log(&accountID, "", actorID, actorName, "account_updated", "account", account.ID.String(), fmt.Sprintf("Updated account %s", account.OwnerName), "", "")

	s.db.Preload("Users").Where("id = ?", accountID).First(&account)
	return &account, nil
}

func (s *AccountService) DeleteAccount(accountID uuid.UUID, actor *models.User) error {
	var account models.Account
	if err := s.db.Where("id = ?", accountID).First(&account).Error; err != nil {
		return errors.New("account not found")
	}

	s.db.Where("account_id = ?", accountID).Delete(&models.User{})

	if err := s.db.Delete(&account).Error; err != nil {
		return err
	}

	actorID := uuid.Nil
	actorName := "System"
	if actor != nil {
		actorID = actor.ID
		actorName = actor.Name
	}
	s.auditService.Log(&accountID, "", actorID, actorName, "account_deleted", "account", account.ID.String(), fmt.Sprintf("Deleted account %s (%s)", account.OwnerName, account.OwnerEmail), "", "")

	return nil
}

func (s *AccountService) AdminUpdatePassword(accountID uuid.UUID, newPassword string, actor *models.User) error {
	if len(newPassword) < 8 {
		return errors.New("password must be at least 8 characters long")
	}

	var user models.User
	err := s.db.Where("account_id = ? AND system_role = ?", accountID, models.RoleOwner).First(&user).Error
	if err != nil {
		if errUser := s.db.Where("id = ?", accountID).First(&user).Error; errUser != nil {
			return errors.New("user not found for this account")
		}
	}

	hashedPw, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	user.PasswordHash = string(hashedPw)
	user.ResetPasswordToken = ""
	user.ResetPasswordExpiresAt = nil

	if err := s.db.Save(&user).Error; err != nil {
		return err
	}

	s.db.Model(&models.Notification{}).Where("recipient_role = 'admin' AND (user_email = ? OR user_id = ?)", user.Email, user.ID).Updates(map[string]interface{}{
		"read":   true,
		"status": "resolved",
	})

	loginURL := s.frontendURL + "/login"
	go s.emailService.SendPasswordUpdatedNotification(user.Email, user.Name, newPassword, loginURL)

	userNotif := models.Notification{
		AccountID:     user.AccountID,
		UserID:        &user.ID,
		Type:          "password_updated",
		Title:         "Password Updated",
		Message:       "Your password was updated by the System Administrator. An email has been sent with your credentials.",
		RecipientRole: "user",
		Status:        "resolved",
	}
	s.db.Create(&userNotif)

	actorID := uuid.Nil
	actorName := "System"
	if actor != nil {
		actorID = actor.ID
		actorName = actor.Name
	}
	s.auditService.Log(user.AccountID, "", actorID, actorName, "admin_password_updated", "user", user.ID.String(), fmt.Sprintf("Admin reset password for %s (%s)", user.Name, user.Email), "", "")

	return nil
}

func (s *AccountService) ListAdminNotifications() ([]models.Notification, error) {
	var notifs []models.Notification
	err := s.db.Where("recipient_role = 'admin'").Order("created_at DESC").Limit(30).Find(&notifs).Error
	if err != nil {
		return nil, err
	}
	return notifs, nil
}

func (s *AccountService) MarkNotificationRead(id uuid.UUID) error {
	return s.db.Model(&models.Notification{}).Where("id = ?", id).Update("read", true).Error
}

func (s *AccountService) ResolveNotification(id uuid.UUID) error {
	return s.db.Model(&models.Notification{}).Where("id = ?", id).Updates(map[string]interface{}{
		"read":   true,
		"status": "resolved",
	}).Error
}

type AdminStats struct {
	TotalAccounts  int64 `json:"totalAccounts"`
	TotalUsers     int64 `json:"totalUsers"`
	ActiveAccounts int64 `json:"activeAccounts"`
	TotalHours     int64 `json:"totalHours"`
}

func (s *AccountService) GetAdminStats() (*AdminStats, error) {
	var stats AdminStats
	s.db.Model(&models.Account{}).Count(&stats.TotalAccounts)
	s.db.Model(&models.User{}).Count(&stats.TotalUsers)
	s.db.Model(&models.Account{}).Where("status = ?", models.StatusActive).Count(&stats.ActiveAccounts)

	var totalSeconds int64
	s.db.Model(&models.Transcript{}).Select("COALESCE(SUM(duration), 0)").Scan(&totalSeconds)
	stats.TotalHours = totalSeconds / 3600

	return &stats, nil
}
