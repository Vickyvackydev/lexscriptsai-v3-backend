package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lexscriptsai-v3-backend/internal/auth"
	"lexscriptsai-v3-backend/internal/config"
	"lexscriptsai-v3-backend/internal/database"
	"lexscriptsai-v3-backend/internal/handlers"
	appMiddleware "lexscriptsai-v3-backend/internal/middleware"
	"lexscriptsai-v3-backend/internal/services"
	"lexscriptsai-v3-backend/pkg/response"

	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
)

func main() {
	log.Println("==================================================")
	log.Println(" LEXSCRIPTSAI V3 — PRODUCTION BACKEND INITIALIZING")
	log.Println("==================================================")

	cfg := config.Load()
	log.Printf("[Init] Environment: %s | Port: %s\n", cfg.Env, cfg.Port)

	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("[Fatal] Failed to connect to database: %v", err)
	}

	tokenService := auth.NewTokenService(cfg.JWTSecret, cfg.JWTAccessExpiryMinutes, cfg.JWTRefreshExpiryDays)
	auditService := services.NewAuditService(db.DB)
	emailService := services.NewEmailService(cfg)
	authService := services.NewAuthService(db.DB, tokenService, auditService, emailService, cfg.FrontendURL)
	accountService := services.NewAccountService(db.DB, auditService, emailService, cfg.FrontendURL)
	locationService := services.NewLocationService(db.DB)
	permissionService := services.NewPermissionService(db.DB, auditService)

	storageService, err := services.NewStorageService(cfg, db.DB)
	if err != nil {
		log.Printf("[Warn] GCS initialization notice: %v", err)
	}

	notificationService := services.NewNotificationService(db.DB)
	whisperService := services.NewWhisperService(cfg)
	transcriptionService := services.NewTranscriptionService(db.DB, whisperService, auditService, storageService, emailService, notificationService)
	transcriptService := services.NewTranscriptService(db.DB, auditService, transcriptionService, storageService)
	folderService := services.NewFolderService(db.DB, auditService)
	causeListService := services.NewCauseListService(db.DB, auditService)
	recycleBinService := services.NewRecycleBinService(db.DB, auditService)
	searchService := services.NewSearchService(db.DB)
	collabService := services.NewCollabService(db.DB, tokenService, emailService, notificationService)
	aiService := services.NewAIService(db.DB, whisperService)

	authHandler := handlers.NewAuthHandler(authService, permissionService, db.DB)
	adminHandler := handlers.NewAdminHandler(accountService, auditService)
	locationHandler := handlers.NewLocationHandler(locationService)
	permissionHandler := handlers.NewPermissionHandler(permissionService)
	uploadHandler := handlers.NewUploadHandler(storageService, db.DB)
	transcriptHandler := handlers.NewTranscriptHandler(transcriptService, db.DB)
	collabHandler := handlers.NewCollabHandler(collabService, aiService, tokenService, db.DB)
	folderHandler := handlers.NewFolderHandler(folderService)
	causeListHandler := handlers.NewCauseListHandler(causeListService)
	recycleBinHandler := handlers.NewRecycleBinHandler(recycleBinService)
	searchHandler := handlers.NewSearchHandler(searchService)
	notificationHandler := handlers.NewNotificationHandler(notificationService, tokenService, db.DB)

	e := echo.New()
	e.HideBanner = true

	e.Use(echoMiddleware.Recover())
	e.Use(appMiddleware.RequestID())
	e.Use(appMiddleware.SecurityHeaders())
	e.Use(appMiddleware.StructuredLogger())

	e.Use(echoMiddleware.CORSWithConfig(echoMiddleware.CORSConfig{
		AllowOrigins:     []string{cfg.FrontendURL, "http://localhost:5173", "http://localhost:3000"},
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization, "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           86400,
	}))

	e.GET("/health", func(c echo.Context) error {
		return response.Success(c, http.StatusOK, map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"version":   "3.0.0",
		})
	})

	e.GET("/ready", func(c echo.Context) error {
		sqlDB, err := db.DB.DB()
		if err != nil || sqlDB.Ping() != nil {
			return response.Error(c, http.StatusServiceUnavailable, "DB_DOWN", "Database ping failed", nil)
		}
		return response.Success(c, http.StatusOK, map[string]string{"status": "ready"})
	})

	e.Static("/uploads", "./uploads")

	v1 := e.Group("/api/v1")

	// Public Auth
	v1.POST("/auth/login", authHandler.Login)
	v1.POST("/auth/refresh", authHandler.Refresh)
	v1.POST("/auth/forgot-password", authHandler.ForgotPassword)
	v1.POST("/auth/reset-password", authHandler.ResetPassword)

	// Authenticated Core
	authMiddleware := appMiddleware.RequireAuth(tokenService, db.DB)

	authenticated := v1.Group("", authMiddleware)
	authenticated.GET("/auth/me", authHandler.GetMe)
	authenticated.GET("/users/preferences", authHandler.GetPreferences)
	authenticated.PUT("/users/preferences", authHandler.UpdatePreferences)
	authenticated.PATCH("/users/preferences", authHandler.UpdatePreferences)
	authenticated.GET("/user/preferences", authHandler.GetPreferences)
	authenticated.PUT("/user/preferences", authHandler.UpdatePreferences)
	authenticated.PATCH("/user/preferences", authHandler.UpdatePreferences)
	authenticated.GET("/users/settings", authHandler.GetPreferences)
	authenticated.PUT("/users/settings", authHandler.UpdatePreferences)
	authenticated.PATCH("/users/settings", authHandler.UpdatePreferences)
	authenticated.GET("/user/settings", authHandler.GetPreferences)
	authenticated.PUT("/user/settings", authHandler.UpdatePreferences)
	authenticated.PATCH("/user/settings", authHandler.UpdatePreferences)
	authenticated.PUT("/users/profile", authHandler.UpdateProfile)
	authenticated.PUT("/users/password", authHandler.ChangePassword)
	authenticated.GET("/locations", locationHandler.ListLocations)
	authenticated.GET("/search", searchHandler.GlobalSearch)

	// User Notifications Endpoints (Owners, Sub-Accounts, Admins)
	authenticated.GET("/notifications", notificationHandler.ListUserNotifications)
	authenticated.PATCH("/notifications/:id/read", notificationHandler.MarkNotificationRead)
	authenticated.POST("/notifications/read-all", notificationHandler.MarkAllRead)
	authenticated.DELETE("/notifications/:id", notificationHandler.DeleteNotification)

	// Admin Protected Endpoints
	adminMiddleware := appMiddleware.RequireAdmin()
	adminGroup := authenticated.Group("/admin", adminMiddleware)
	{
		adminGroup.GET("/stats", adminHandler.GetStats)
		adminGroup.GET("/logs", adminHandler.GetLogs)
		adminGroup.DELETE("/logs", adminHandler.ClearLogs)
		adminGroup.GET("/accounts", adminHandler.ListAccounts)
		adminGroup.GET("/accounts/:id", adminHandler.GetAccount)
		adminGroup.POST("/accounts", adminHandler.CreateAccount)
		adminGroup.PUT("/accounts/:id", adminHandler.UpdateAccount)
		adminGroup.DELETE("/accounts/:id", adminHandler.DeleteAccount)
		adminGroup.POST("/accounts/:id/password", adminHandler.AdminUpdatePassword)
		adminGroup.POST("/accounts/:id/sub-accounts", adminHandler.AddSubAccount)
		adminGroup.PATCH("/users/:id/status", adminHandler.UpdateUserStatus)
		adminGroup.PATCH("/accounts/:id/status", adminHandler.UpdateAccountStatus)
		adminGroup.GET("/notifications", adminHandler.ListNotifications)
		adminGroup.PATCH("/notifications/:id/read", adminHandler.MarkNotificationRead)
		adminGroup.POST("/notifications/:id/resolve", adminHandler.ResolveNotification)
		adminGroup.POST("/locations", locationHandler.CreateLocation)
		adminGroup.PUT("/locations/:id", locationHandler.UpdateLocation)
		adminGroup.DELETE("/locations/:id", locationHandler.DeleteLocation)
		adminGroup.GET("/search", searchHandler.AdminSearch)
	}

	// Account Owner Protected Endpoints
	ownerMiddleware := appMiddleware.RequireAccountOwner()
	ownerGroup := authenticated.Group("/accounts", ownerMiddleware)
	{
		ownerGroup.GET("/members", permissionHandler.ListAccountMembers)
		ownerGroup.GET("/members/:userId/permissions", permissionHandler.GetMemberPermissions)
		ownerGroup.PUT("/members/:userId/permissions", permissionHandler.UpdateMemberPermissions)
	}

	// File Storage Endpoints
	authenticated.POST("/files/upload-url", uploadHandler.GenerateUploadURL, appMiddleware.RequirePermission(db.DB, "upload"))
	authenticated.POST("/files/direct", uploadHandler.DirectUpload, appMiddleware.RequirePermission(db.DB, "upload"))
	authenticated.GET("/files/:id/signed-url", uploadHandler.GetDownloadSignedURL, appMiddleware.RequirePermission(db.DB, "export"))

	// Transcript Endpoints
	authenticated.GET("/transcripts", transcriptHandler.ListTranscripts, appMiddleware.RequirePermission(db.DB, "view"))
	authenticated.GET("/transcripts/:id", transcriptHandler.GetTranscript, appMiddleware.RequirePermission(db.DB, "view"))
	authenticated.POST("/transcripts", transcriptHandler.CreateTranscript, appMiddleware.RequirePermission(db.DB, "upload"))
	authenticated.PATCH("/transcripts/:id", transcriptHandler.UpdateTranscript, appMiddleware.RequirePermission(db.DB, "edit"))
	authenticated.DELETE("/transcripts/:id", transcriptHandler.DeleteTranscript, appMiddleware.RequirePermission(db.DB, "delete"))
	authenticated.POST("/transcripts/:id/retry", transcriptHandler.RetryTranscript, appMiddleware.RequirePermission(db.DB, "edit"))
	authenticated.GET("/transcripts/:id/audio", transcriptHandler.DownloadAudio, appMiddleware.RequirePermission(db.DB, "view"))

	// Transcript AI & Collaboration Endpoints
	authenticated.POST("/transcripts/:id/translate", collabHandler.TranslateTranscript, appMiddleware.RequirePermission(db.DB, "view"))
	authenticated.POST("/transcripts/:id/summary", collabHandler.GenerateSummary, appMiddleware.RequirePermission(db.DB, "view"))
	authenticated.POST("/transcripts/:id/share", collabHandler.ShareTranscript, appMiddleware.RequirePermission(db.DB, "edit"))
	authenticated.GET("/transcripts/:id/collaborators", collabHandler.ListCollaborators, appMiddleware.RequirePermission(db.DB, "view"))
	authenticated.DELETE("/transcripts/:id/collaborators/:userId", collabHandler.RemoveCollaborator, appMiddleware.RequirePermission(db.DB, "edit"))
	authenticated.GET("/transcripts/shared-with-me", collabHandler.ListSharedTranscripts)

	// WebSocket endpoint for real-time presence and collaboration
	v1.GET("/ws/transcripts/:id", collabHandler.HandleWebSocket)

	// WebSocket endpoint for real-time notifications
	v1.GET("/ws/notifications", notificationHandler.HandleWebSocket)

	// Webhook endpoint for transcription callbacks
	v1.POST("/webhooks/transcription", notificationHandler.TranscriptionWebhook)

	// Folder Endpoints
	authenticated.GET("/folders", folderHandler.ListFolders)
	authenticated.POST("/folders", folderHandler.CreateFolder)
	authenticated.PATCH("/folders/:id", folderHandler.RenameFolder)
	authenticated.DELETE("/folders/:id", folderHandler.DeleteFolder)

	// Cause List Endpoints
	authenticated.GET("/cause-lists", causeListHandler.ListCauseLists)
	authenticated.POST("/cause-lists", causeListHandler.CreateCauseList)
	authenticated.GET("/cause-lists/matters", causeListHandler.ListMatters)
	authenticated.POST("/cause-lists/matters", causeListHandler.CreateMatter)
	authenticated.PATCH("/cause-lists/matters/:id/adjourn", causeListHandler.AdjournMatter)
	authenticated.DELETE("/cause-lists/matters/:id", causeListHandler.DeleteMatter)

	// Direct Matters Endpoints (/api/v1/matters)
	authenticated.GET("/matters", causeListHandler.ListMatters)
	authenticated.POST("/matters", causeListHandler.CreateMatter)
	authenticated.POST("/matters/:id/adjourn", causeListHandler.AdjournMatter)
	authenticated.PATCH("/matters/:id/adjourn", causeListHandler.AdjournMatter)
	authenticated.DELETE("/matters/:id", causeListHandler.DeleteMatter)

	// Recycle Bin Endpoints
	authenticated.GET("/recycle-bin", recycleBinHandler.ListRecycleBin)
	authenticated.POST("/recycle-bin/:id/restore", recycleBinHandler.Restore)
	authenticated.DELETE("/recycle-bin/:id", recycleBinHandler.PermanentDelete)

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("[Server] LexScriptsAI V3 API listening on port :%s", cfg.Port)
		if err := e.Start(fmt.Sprintf(":%s", cfg.Port)); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		log.Fatalf("[Fatal] Server startup failed: %v", err)
	case sig := <-shutdown:
		log.Printf("[Shutdown] Signal received: %v. Initiating graceful shutdown...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := e.Shutdown(ctx); err != nil {
			log.Printf("[Error] Error during graceful shutdown: %v", err)
			_ = e.Close()
		}
		log.Println("[Shutdown] Server gracefully terminated.")
	}
}
