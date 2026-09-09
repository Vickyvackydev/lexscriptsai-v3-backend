package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                   string
	BaseURL                string
	Env                    string
	FrontendURL            string
	DatabaseURL            string
	JWTSecret              string
	JWTAccessExpiryMinutes int
	JWTRefreshExpiryDays   int
	WhisperAPIURL          string
	WhisperBaseToken       string
	GCSBucketName          string
	GoogleAppCredentials   string
	AdminDefaultEmail      string
	AdminDefaultPassword   string
	BrevoAPIKey            string
	BrevoSenderEmail       string
	BrevoSenderName        string
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("[Config] .env file not found or could not be loaded, using environment variables")
	}

	return &Config{
		Port:                   getEnv("PORT", "8080"),
		BaseURL:                getEnv("BASE_URL", "http://localhost:8080"),
		Env:                    getEnv("ENV", "development"),
		FrontendURL:            getEnv("FRONTEND_URL", "http://localhost:5173"),
		DatabaseURL:            getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/lexscriptsai_v3?sslmode=disable"),
		JWTSecret:              getEnv("JWT_SECRET", "lexscriptsai-v3-super-secret-production-grade-jwt-key-2026"),
		JWTAccessExpiryMinutes: getEnvAsInt("JWT_ACCESS_EXPIRY_MINUTES", 15),
		JWTRefreshExpiryDays:   getEnvAsInt("JWT_REFRESH_EXPIRY_DAYS", 7),
		WhisperAPIURL:          getEnv("WHISPER_API_URL", "http://194.93.48.51:8070"),
		WhisperBaseToken:       getEnv("WHISPER_BASE_TOKEN", "RlU7oyEnR9yuqGLBWdLk1vkwSx3U7d_bMlQPh3YIJjs"),
		GCSBucketName:          getEnv("GCS_BUCKET_NAME", "exscripts-ai-media"),
		GoogleAppCredentials:   getEnv("GOOGLE_APPLICATION_CREDENTIALS", "service-account.json"),
		AdminDefaultEmail:      getEnv("ADMIN_DEFAULT_EMAIL", "admin@lexscriptsai.com"),
		AdminDefaultPassword:   getEnv("ADMIN_DEFAULT_PASSWORD", "AdminPassword2026!#"),
		BrevoAPIKey:            getEnv("BREVO_API_KEY", ""),
		BrevoSenderEmail:       getEnv("BREVO_SENDER_EMAIL", "noreply@lexscriptsai.com"),
		BrevoSenderName:        getEnv("BREVO_SENDER_NAME", "LexScriptsAI"),
	}
}

func getEnv(key string, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	if val, ok := os.LookupEnv(key); ok {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return fallback
}
