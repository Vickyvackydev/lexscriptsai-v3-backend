package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type BaseUUIDModel struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deletedAt,omitempty"`
}

type AccountStatus string

const (
	StatusActive    AccountStatus = "active"
	StatusDisabled  AccountStatus = "disabled"
	StatusSuspended AccountStatus = "suspended"
)

type SystemRole string

const (
	RoleAdmin      SystemRole = "admin"
	RoleOwner      SystemRole = "owner"
	RoleSubAccount SystemRole = "sub_account"
)

type ProfessionalRole string

const (
	RoleJudge         ProfessionalRole = "judge"
	RoleCourtReporter ProfessionalRole = "court_reporter"
	RoleScopist       ProfessionalRole = "scopist"
)

type Account struct {
	BaseUUIDModel
	OwnerFirstName  string        `gorm:"size:100" json:"ownerFirstName"`
	OwnerLastName   string        `gorm:"size:100" json:"ownerLastName"`
	OwnerName       string        `gorm:"size:200;not null" json:"ownerName"`
	OwnerEmail      string        `gorm:"size:255;uniqueIndex;not null" json:"ownerEmail"`
	OwnerRole       string        `gorm:"size:100;default:'Account Owner'" json:"ownerRole"`
	LocationID      *uuid.UUID    `gorm:"type:uuid;index" json:"locationId,omitempty"`
	Location        string        `gorm:"size:150" json:"location,omitempty"`
	Status          AccountStatus `gorm:"size:50;default:'active';not null" json:"status"`
	UserCount       int           `gorm:"default:0" json:"userCount"`
	TranscriptCount int           `gorm:"default:0" json:"transcriptCount"`
	HoursProcessed  float64       `gorm:"type:numeric(10,2);default:0" json:"hoursProcessed"`
	LastActivity    *time.Time    `json:"lastActivity,omitempty"`
	Users           []User        `gorm:"foreignKey:AccountID" json:"users,omitempty"`
}

type User struct {
	BaseUUIDModel
	AccountID        *uuid.UUID       `gorm:"type:uuid;index" json:"accountId,omitempty"`
	FirstName        string           `gorm:"size:100" json:"firstName"`
	LastName         string           `gorm:"size:100" json:"lastName"`
	Name             string           `gorm:"size:200;not null" json:"name"`
	Email            string           `gorm:"size:255;uniqueIndex;not null" json:"email"`
	PasswordHash     string           `gorm:"size:255;not null" json:"-"`
	SystemRole       SystemRole       `gorm:"size:50;not null" json:"systemRole"`
	ProfessionalRole ProfessionalRole `gorm:"size:50" json:"role"`
	LocationID       *uuid.UUID       `gorm:"type:uuid;index" json:"locationId,omitempty"`
	Location         string           `gorm:"size:150" json:"location,omitempty"`
	Status           AccountStatus    `gorm:"size:50;default:'active';not null" json:"status"`
	AvatarInitials   string           `gorm:"size:10" json:"avatarInitials"`
	AutoSave         bool             `gorm:"default:true" json:"autoSave"`
	AutoDownload     bool             `gorm:"default:true" json:"autoDownload"`
	LastActive             *time.Time `json:"lastActive,omitempty"`
	ResetPasswordToken     string     `gorm:"size:255;index" json:"-"`
	ResetPasswordExpiresAt *time.Time `json:"-"`
}

type Location struct {
	BaseUUIDModel
	Name      string `gorm:"size:150;uniqueIndex;not null" json:"name"`
	State     string `gorm:"size:150" json:"state"`
	UserCount int    `gorm:"default:0" json:"userCount"`
	IsActive  bool   `gorm:"default:true" json:"isActive"`
}

type StringSlice []string

func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	return json.Marshal(s)
}

func (s *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = StringSlice{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("type assertion to []byte/string failed")
	}
	return json.Unmarshal(bytes, s)
}

type Float64Slice []float64

func (f Float64Slice) Value() (driver.Value, error) {
	return json.Marshal(f)
}

func (f *Float64Slice) Scan(value interface{}) error {
	if value == nil {
		*f = Float64Slice{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("type assertion to []byte/string failed")
	}
	return json.Unmarshal(bytes, f)
}

type MemberPermission struct {
	BaseUUIDModel
	AccountID               uuid.UUID   `gorm:"type:uuid;index;not null" json:"accountId"`
	UserID                  uuid.UUID   `gorm:"type:uuid;uniqueIndex;not null" json:"userId"`
	Permissions             StringSlice `gorm:"type:jsonb;not null" json:"permissions"`
	AccessibleTranscriptIDs string      `gorm:"type:text;default:'all';not null" json:"accessibleTranscriptIds"`
}

type File struct {
	BaseUUIDModel
	AccountID        uuid.UUID `gorm:"type:uuid;index;not null" json:"accountId"`
	OwnerID          uuid.UUID `gorm:"type:uuid;index;not null" json:"ownerId"`
	ObjectKey        string    `gorm:"size:500;uniqueIndex;not null" json:"objectKey"`
	OriginalFilename string    `gorm:"size:255;not null" json:"originalFilename"`
	ContentType      string    `gorm:"size:100;not null" json:"contentType"`
	SizeBytes        int64     `gorm:"not null" json:"sizeBytes"`
	Status           string    `gorm:"size:50;default:'pending';not null" json:"status"`
}

type Word struct {
	ID         string  `json:"id"`
	Text       string  `json:"text"`
	StartTime  float64 `json:"startTime"`
	EndTime    float64 `json:"endTime"`
	Confidence float64 `json:"confidence"`
	Tag        string  `json:"tag,omitempty"`
}

type SpeakerBank struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Words          []Word `json:"words"`
	ParagraphIndex int    `json:"paragraphIndex"`
}

type SpeakerBanks []SpeakerBank

func (sb SpeakerBanks) Value() (driver.Value, error) {
	return json.Marshal(sb)
}

func (sb *SpeakerBanks) Scan(value interface{}) error {
	if value == nil {
		*sb = SpeakerBanks{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("type assertion to []byte/string failed")
	}
	return json.Unmarshal(bytes, sb)
}

type TranscriptStatus string

const (
	TranscriptUploading  TranscriptStatus = "uploading"
	TranscriptProcessing TranscriptStatus = "processing"
	TranscriptCompleted  TranscriptStatus = "completed"
	TranscriptFailed     TranscriptStatus = "failed"
	TranscriptDraft      TranscriptStatus = "draft"
)

type Transcript struct {
	BaseUUIDModel
	AccountID     uuid.UUID        `gorm:"type:uuid;index;not null" json:"accountId"`
	OwnerID       uuid.UUID        `gorm:"type:uuid;index;not null" json:"ownerId"`
	FolderID      *uuid.UUID       `gorm:"type:uuid;index" json:"folderId,omitempty"`
	MatterID      *uuid.UUID       `gorm:"type:uuid;index" json:"matterId,omitempty"`
	AudioFileID   *uuid.UUID       `gorm:"type:uuid;index" json:"audioFileId,omitempty"`
	AudioURL      string           `gorm:"size:1000" json:"audioUrl,omitempty"`
	Title         string           `gorm:"size:255;not null" json:"title"`
	Status        TranscriptStatus `gorm:"size:50;default:'processing';not null" json:"status"`
	Duration      int              `gorm:"default:0" json:"duration"`
	WordCount     int              `gorm:"default:0" json:"wordCount"`
	Language      string           `gorm:"size:50;default:'en'" json:"language"`
	SpeakerBanks  SpeakerBanks     `gorm:"type:jsonb" json:"speakerBanks"`
	Flags         Float64Slice     `gorm:"type:jsonb" json:"flags,omitempty"`
	Source        string           `gorm:"size:50;default:'upload';index" json:"source,omitempty"`
	IsTrashed     bool             `gorm:"default:false;index" json:"isTrashed"`
	DeletedByID   *uuid.UUID       `gorm:"type:uuid" json:"deletedById,omitempty"`
	DeletedByName string           `gorm:"size:200" json:"deletedByName,omitempty"`
	ExpiresAt     *time.Time       `json:"expiresAt,omitempty"`
	UserRole      string           `gorm:"-" json:"userRole,omitempty"`
}

type JobStatus string

const (
	JobQueued     JobStatus = "queued"
	JobProcessing JobStatus = "processing"
	JobCompleted  JobStatus = "completed"
	JobFailed     JobStatus = "failed"
)

type TranscriptionJob struct {
	BaseUUIDModel
	AccountID    uuid.UUID  `gorm:"type:uuid;index;not null" json:"accountId"`
	TranscriptID uuid.UUID  `gorm:"type:uuid;index;not null" json:"transcriptId"`
	FileID       uuid.UUID  `gorm:"type:uuid;index;not null" json:"fileId"`
	AudioURL     string     `gorm:"size:1000;not null" json:"audioUrl"`
	ExternalID   string     `gorm:"size:255;index" json:"externalId,omitempty"`
	Status       JobStatus  `gorm:"size:50;default:'queued';not null" json:"status"`
	RetryCount   int        `gorm:"default:0" json:"retryCount"`
	ErrorMessage string     `gorm:"type:text" json:"errorMessage,omitempty"`
	Language     string     `gorm:"size:50;default:'en'" json:"language"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

type Folder struct {
	BaseUUIDModel
	AccountID       uuid.UUID `gorm:"type:uuid;index;not null" json:"accountId"`
	OwnerID         uuid.UUID `gorm:"type:uuid;index;not null" json:"ownerId"`
	Name            string    `gorm:"size:200;not null" json:"name"`
	TranscriptCount int       `gorm:"default:0" json:"transcriptCount"`
	IsTrashed       bool      `gorm:"default:false;index" json:"isTrashed"`
}

type MatterType string

const (
	MatterHearing  MatterType = "Hearing"
	MatterMention  MatterType = "Mention"
	MatterMotion   MatterType = "Motion"
	MatterRuling   MatterType = "Ruling"
	MatterJudgment MatterType = "Judgment"
)

type MatterStatus string

const (
	MatterStatusScheduled  MatterStatus = "Scheduled"
	MatterStatusInProgress MatterStatus = "In Progress"
	MatterStatusHeard      MatterStatus = "Heard"
	MatterStatusAdjourned  MatterStatus = "Adjourned"
)

type CauseList struct {
	BaseUUIDModel
	AccountID   uuid.UUID       `gorm:"type:uuid;index;not null" json:"accountId"`
	OwnerID     uuid.UUID       `gorm:"type:uuid;index;not null" json:"ownerId"`
	Name        string          `gorm:"size:200;not null" json:"name"`
	FolderID    *uuid.UUID      `gorm:"type:uuid;index" json:"folderId,omitempty"`
	FolderName  string          `gorm:"size:200" json:"folderName,omitempty"`
	ScheduledAt *time.Time      `json:"scheduledAt,omitempty"`
	Items       []CauseListItem `gorm:"foreignKey:CauseListID" json:"items,omitempty"`
}

type CauseListItem struct {
	BaseUUIDModel
	AccountID       uuid.UUID    `gorm:"type:uuid;index;not null" json:"accountId"`
	CauseListID     *uuid.UUID   `gorm:"type:uuid;index" json:"causeListId,omitempty"`
	CauseListName   string       `gorm:"size:200" json:"causeListName,omitempty"`
	Date            string       `gorm:"size:20;index;not null" json:"date"`
	CaseNumber      string       `gorm:"size:150;not null" json:"caseNumber"`
	Parties         string       `gorm:"size:255;not null" json:"parties"`
	MatterType      MatterType   `gorm:"size:50;not null" json:"matterType"`
	Time            string       `gorm:"size:50" json:"time,omitempty"`
	Status          MatterStatus `gorm:"size:50;default:'Scheduled';not null" json:"status"`
	AdjournedToDate string       `gorm:"size:20" json:"adjournedToDate,omitempty"`
	AdjournedReason string       `gorm:"type:text" json:"adjournedReason,omitempty"`
	FolderID        *uuid.UUID   `gorm:"type:uuid;index" json:"folderId,omitempty"`
	FolderName      string       `gorm:"size:200" json:"folderName,omitempty"`
	Notes           string       `gorm:"type:text" json:"notes,omitempty"`
}

type AuditLog struct {
	BaseUUIDModel
	AccountID    *uuid.UUID `gorm:"type:uuid;index" json:"accountId,omitempty"`
	AccountName  string     `gorm:"size:200" json:"accountName,omitempty"`
	ActorID      uuid.UUID  `gorm:"type:uuid;index;not null" json:"actorId"`
	ActorName    string     `gorm:"size:200;not null" json:"actorName"`
	Action       string     `gorm:"size:100;not null;index" json:"action"`
	ResourceType string     `gorm:"size:100" json:"resourceType,omitempty"`
	ResourceID   string     `gorm:"size:100" json:"resourceId,omitempty"`
	Details      string     `gorm:"type:text" json:"details,omitempty"`
	IPAddress    string     `gorm:"size:50" json:"ipAddress,omitempty"`
	UserAgent    string     `gorm:"size:255" json:"userAgent,omitempty"`
}

type Notification struct {
	BaseUUIDModel
	AccountID     *uuid.UUID `gorm:"type:uuid;index" json:"accountId,omitempty"`
	UserID        *uuid.UUID `gorm:"type:uuid;index" json:"userId,omitempty"`
	Type          string     `gorm:"size:50;not null" json:"type"`
	Title         string     `gorm:"size:200;not null" json:"title"`
	Message       string     `gorm:"type:text;not null" json:"message"`
	Read          bool       `gorm:"default:false;index" json:"read"`
	ActionURL     string     `gorm:"size:500" json:"actionUrl,omitempty"`
	ActorName     string     `gorm:"size:200" json:"actorName,omitempty"`
	RecipientRole string     `gorm:"size:50;default:'user';index" json:"recipientRole"`
	Status        string     `gorm:"size:50;default:'pending'" json:"status,omitempty"`
	UserEmail     string     `gorm:"size:255" json:"userEmail,omitempty"`
}

type TranscriptShareRole string

const (
	ShareRoleViewer TranscriptShareRole = "viewer"
	ShareRoleEditor TranscriptShareRole = "editor"
)

type TranscriptShare struct {
	BaseUUIDModel
	TranscriptID uuid.UUID           `gorm:"type:uuid;index;not null" json:"transcriptId"`
	OwnerID      uuid.UUID           `gorm:"type:uuid;index;not null" json:"ownerId"`
	SharedWithID uuid.UUID           `gorm:"type:uuid;index;not null" json:"sharedWithId"`
	UserEmail    string              `gorm:"size:255;not null" json:"userEmail"`
	UserName     string              `gorm:"size:200" json:"userName"`
	Role         TranscriptShareRole `gorm:"size:50;default:'editor';not null" json:"role"`
}

