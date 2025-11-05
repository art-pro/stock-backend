package config

import "os"

// Config holds all application configuration
type Config struct {
	AppEnv                string
	Port                  string
	FrontendURL           string
	AdminUsername         string
	AdminPassword         string
	JWTSecret             string
	DatabasePath          string
	AlphaVantageAPIKey    string
	XAIAPIKey             string
	ExchangeRatesAPIKey   string
	SendGridAPIKey        string
	AlertEmailFrom        string
	AlertEmailTo          string
	EnableScheduler       bool
	DefaultUpdateFrequency string
}

// Load reads configuration from environment variables
func Load() *Config {
	enableScheduler := os.Getenv("ENABLE_SCHEDULER") == "true"
	
	return &Config{
		AppEnv:                getEnv("APP_ENV", "development"),
		Port:                  getEnv("PORT", "8080"),
		FrontendURL:           getEnv("FRONTEND_URL", "http://localhost:3000"),
		AdminUsername:         getEnv("ADMIN_USERNAME", "artpro"),
		AdminPassword:         getEnv("ADMIN_PASSWORD", "defaultPasswordLaterProvided"),
		JWTSecret:             getEnv("JWT_SECRET", "change-this-secret-in-production"),
		DatabasePath:          getEnv("DATABASE_PATH", "./data/stocks.db"),
		AlphaVantageAPIKey:    os.Getenv("ALPHA_VANTAGE_API_KEY"),
		XAIAPIKey:             os.Getenv("XAI_API_KEY"),
		ExchangeRatesAPIKey:   os.Getenv("EXCHANGE_RATES_API_KEY"),
		SendGridAPIKey:        os.Getenv("SENDGRID_API_KEY"),
		AlertEmailFrom:        os.Getenv("ALERT_EMAIL_FROM"),
		AlertEmailTo:          os.Getenv("ALERT_EMAIL_TO"),
		EnableScheduler:       enableScheduler,
		DefaultUpdateFrequency: getEnv("DEFAULT_UPDATE_FREQUENCY", "daily"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

