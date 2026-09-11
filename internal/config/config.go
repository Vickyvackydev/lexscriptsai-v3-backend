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
	WhisperPrimaryAPIURL   string
	WhisperPrimaryToken    string
	WhisperFallbackAPIURL  string
	WhisperFallbackToken   string
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

	primaryURL := getEnv("WHISPER_PRIMARY_API_URL", getEnv("WHISPER_API_URL", ""))
	primaryToken := getEnv("WHISPER_PRIMARY_BASE_TOKEN", getEnv("WHISPER_BASE_TOKEN", ""))
	fallbackURL := getEnv("WHISPER_FALLBACK_API_URL", "")
	fallbackToken := getEnv("WHISPER_FALLBACK_BASE_TOKEN", "")

	return &Config{
		Port:                   getEnv("PORT", "8080"),
		BaseURL:                getEnv("BASE_URL", "http://localhost:8080"),
		Env:                    getEnv("ENV", "development"),
		FrontendURL:            getEnv("FRONTEND_URL", "http://localhost:5173"),
		DatabaseURL:            getEnv("DATABASE_URL", ""),
		JWTSecret:              getEnv("JWT_SECRET", ""),
		JWTAccessExpiryMinutes: getEnvAsInt("JWT_ACCESS_EXPIRY_MINUTES", 15),
		JWTRefreshExpiryDays:   getEnvAsInt("JWT_REFRESH_EXPIRY_DAYS", 7),
		WhisperPrimaryAPIURL:   primaryURL,
		WhisperPrimaryToken:    primaryToken,
		WhisperFallbackAPIURL:  fallbackURL,
		WhisperFallbackToken:   fallbackToken,
		WhisperAPIURL:          primaryURL,
		WhisperBaseToken:       primaryToken,
		GCSBucketName:          getEnv("GCS_BUCKET_NAME", ""),
		GoogleAppCredentials:   getEnv("GOOGLE_APPLICATION_CREDENTIALS", ""),
		AdminDefaultEmail:      getEnv("ADMIN_DEFAULT_EMAIL", "admin@lexscriptsai.com"),
		AdminDefaultPassword:   getEnv("ADMIN_DEFAULT_PASSWORD", ""),
		BrevoAPIKey:            getEnv("BREVO_API_KEY", ""),
		BrevoSenderEmail:       getEnv("BREVO_SENDER_EMAIL", ""),
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
