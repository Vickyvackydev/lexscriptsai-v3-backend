package auth

import (
	"errors"
	"fmt"
	"time"

	"lexscriptsai-v3-backend/internal/models"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type JWTClaims struct {
	UserID           uuid.UUID               `json:"userId"`
	AccountID        *uuid.UUID              `json:"accountId,omitempty"`
	Email            string                  `json:"email"`
	SystemRole       models.SystemRole       `json:"systemRole"`
	ProfessionalRole models.ProfessionalRole `json:"role"`
	TokenType        string                  `json:"tokenType"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"`
}

type TokenService struct {
	secretKey              []byte
	accessExpiryDuration   time.Duration
	refreshExpiryDuration  time.Duration
}

func NewTokenService(secretKey string, accessMinutes int, refreshDays int) *TokenService {
	return &TokenService{
		secretKey:             []byte(secretKey),
		accessExpiryDuration:  time.Duration(accessMinutes) * time.Minute,
		refreshExpiryDuration: time.Duration(refreshDays) * 24 * time.Hour,
	}
}

func (s *TokenService) GenerateTokenPair(user *models.User) (*TokenPair, error) {
	now := time.Now().UTC()

	accessExpiry := now.Add(s.accessExpiryDuration)
	accessClaims := &JWTClaims{
		UserID:           user.ID,
		AccountID:        user.AccountID,
		Email:            user.Email,
		SystemRole:       user.SystemRole,
		ProfessionalRole: user.ProfessionalRole,
		TokenType:        "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(accessExpiry),
			Issuer:    "lexscriptsai-v3",
		},
	}

	accessTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessToken, err := accessTokenObj.SignedString(s.secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	refreshExpiry := now.Add(s.refreshExpiryDuration)
	refreshClaims := &JWTClaims{
		UserID:           user.ID,
		AccountID:        user.AccountID,
		Email:            user.Email,
		SystemRole:       user.SystemRole,
		ProfessionalRole: user.ProfessionalRole,
		TokenType:        "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(refreshExpiry),
			Issuer:    "lexscriptsai-v3",
		},
	}

	refreshTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshToken, err := refreshTokenObj.SignedString(s.secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.accessExpiryDuration.Seconds()),
	}, nil
}

func (s *TokenService) ValidateToken(tokenString string, expectedType string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secretKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	if expectedType != "" && claims.TokenType != expectedType {
		return nil, fmt.Errorf("token type mismatch: expected %s, got %s", expectedType, claims.TokenType)
	}

	return claims, nil
}
