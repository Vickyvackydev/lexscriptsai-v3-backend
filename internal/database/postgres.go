package database

import (
	"fmt"
	"log"
	"time"

	"lexscriptsai-v3-backend/internal/config"
	"lexscriptsai-v3-backend/internal/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB struct {
	*gorm.DB
}

func Connect(cfg *config.Config) (*DB, error) {
	logLevel := logger.Warn
	if cfg.Env == "development" {
		logLevel = logger.Info
	}

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	}

	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(15 * time.Minute)

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	log.Println("[Database] Connected successfully to PostgreSQL")

	db.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`)
	db.Exec(`CREATE EXTENSION IF NOT EXISTS "pgcrypto";`)

	if err := db.AutoMigrate(
		&models.Location{},
		&models.Account{},
		&models.User{},
		&models.MemberPermission{},
		&models.File{},
		&models.Transcript{},
		&models.TranscriptionJob{},
		&models.Folder{},
		&models.CauseList{},
		&models.CauseListItem{},
		&models.AuditLog{},
		&models.Notification{},
		&models.TranscriptShare{},
	); err != nil {
		return nil, fmt.Errorf("database auto-migration failed: %w", err)
	}

	db.Exec("ALTER TABLE users ALTER COLUMN account_id DROP NOT NULL;")
	db.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS auto_save BOOLEAN DEFAULT TRUE;")
	db.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS auto_download BOOLEAN DEFAULT TRUE;")
	db.Exec("ALTER TABLE transcripts ADD COLUMN IF NOT EXISTS flags JSONB;")
	db.Exec("ALTER TABLE transcripts ADD COLUMN IF NOT EXISTS source VARCHAR(50) DEFAULT 'upload';")

	log.Println("[Database] Schema migrations completed successfully")

	seedDefaults(db, cfg)

	return &DB{DB: db}, nil
}

func seedDefaults(db *gorm.DB, cfg *config.Config) {
	var adminCount int64
	db.Model(&models.User{}).Where("system_role = ?", models.RoleAdmin).Count(&adminCount)
	if adminCount == 0 {
		hashedPw, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminDefaultPassword), bcrypt.DefaultCost)
		if err == nil {
			admin := models.User{
				Name:           "Administrator",
				FirstName:      "System",
				LastName:       "Admin",
				Email:          cfg.AdminDefaultEmail,
				PasswordHash:   string(hashedPw),
				SystemRole:     models.RoleAdmin,
				Status:         models.StatusActive,
				AvatarInitials: "AD",
				AccountID:      nil,
			}
			if err := db.Create(&admin).Error; err != nil {
				log.Printf("[Database] Failed to seed admin user: %v\n", err)
			} else {
				log.Printf("[Database] Seeded default admin user: %s\n", cfg.AdminDefaultEmail)
			}
		}
	}
}
