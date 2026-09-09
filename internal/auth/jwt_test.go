package auth_test

import (
	"testing"
	"time"

	"lexscriptsai-v3-backend/internal/auth"
	"lexscriptsai-v3-backend/internal/models"

	"github.com/google/uuid"
)

func TestTokenService_GenerateAndValidate(t *testing.T) {
	svc := auth.NewTokenService("test-secret-key-that-is-sufficiently-long-for-testing-1234", 15, 7)

	accountID := uuid.New()
	userID := uuid.New()
	user := &models.User{
		BaseUUIDModel:    models.BaseUUIDModel{ID: userID},
		AccountID:        &accountID,
		Email:            "reporter@testcourt.ng",
		SystemRole:       models.RoleSubAccount,
		ProfessionalRole: models.RoleCourtReporter,
	}

	tokens, err := svc.GenerateTokenPair(user)
	if err != nil {
		t.Fatalf("Failed to generate token pair: %v", err)
	}

	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("Tokens cannot be empty")
	}

	// Validates Access Token 
	claims, err := svc.ValidateToken(tokens.AccessToken, "access")
	if err != nil {
		t.Fatalf("Failed to validate access token: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("Expected userID %s, got %s", userID, claims.UserID)
	}
	if claims.AccountID == nil || *claims.AccountID != accountID {
		t.Errorf("Expected accountID %s, got %v", accountID, claims.AccountID)
	}
	if claims.SystemRole != models.RoleSubAccount {
		t.Errorf("Expected system role %s, got %s", models.RoleSubAccount, claims.SystemRole)
	}
	if claims.ProfessionalRole != models.RoleCourtReporter {
		t.Errorf("Expected professional role %s, got %s", models.RoleCourtReporter, claims.ProfessionalRole)
	}

	// Reject if expecting access token but passing refresh token
	_, err = svc.ValidateToken(tokens.RefreshToken, "access")
	if err == nil {
		t.Error("Expected error when validating refresh token as access token, got nil")
	}
}

func TestTokenService_ExpiredToken(t *testing.T) {
	// Create token service with 0 duration
	svc := auth.NewTokenService("test-secret-key-that-is-sufficiently-long-for-testing-1234", 0, 0)

	accID := uuid.New()
	user := &models.User{
		BaseUUIDModel: models.BaseUUIDModel{ID: uuid.New()},
		AccountID:     &accID,
		Email:         "test@test.com",
		SystemRole:    models.RoleOwner,
	}

	tokens, err := svc.GenerateTokenPair(user)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	_, err = svc.ValidateToken(tokens.AccessToken, "access")
	if err == nil {
		t.Error("Expected error for expired token, got nil")
	}
}
