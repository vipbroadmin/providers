package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPPort             string
	MerchantID           string
	MerchantKey          string
	MaxTimestampSkewSecs int64
	WalletsServiceURL    string
	APIURL               string
	DatabaseURL          string
	MigrationsDir        string
	SyncInterval         time.Duration
}

func Load() Config {
	if err := godotenv.Load(); err != nil {
		log.Printf("warning: .env file not found, using environment variables: %v", err)
	}

	return Config{
		HTTPPort:             getEnv("SLOTEGRATOR_HTTP_PORT", "8092"),
		MerchantID:           getEnv("SLOTEGRATOR_MERCHANT_ID", ""),
		MerchantKey:          getEnv("SLOTEGRATOR_MERCHANT_KEY", ""),
		MaxTimestampSkewSecs: getInt64Env("SLOTEGRATOR_MAX_SKEW_SECS", 30),
		WalletsServiceURL:    getEnv("WALLETS_SERVICE_URL", "http://wallets-service:8081"),
		APIURL:               getEnv("SLOTEGRATOR_API_URL", ""),
		DatabaseURL:          getEnv("DATABASE_URL", ""),
		MigrationsDir:        getEnv("MIGRATIONS_DIR", "migrations"),
		SyncInterval:         getDurationEnv("SLOTEGRATOR_SYNC_INTERVAL", 5*time.Minute),
	}
}

func (c Config) Validate() error {
	if c.MerchantID == "" || c.MerchantKey == "" {
		return errConfig("merchant id/key required")
	}
	if c.APIURL == "" {
		return errConfig("api url required")
	}
	if c.DatabaseURL == "" {
		return errConfig("database url required")
	}
	return nil
}

type errConfig string

func (e errConfig) Error() string { return string(e) }

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt64Env(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parsed
		}
	}
	return def
}

func getDurationEnv(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if parsed, err := time.ParseDuration(v); err == nil {
			return parsed
		}
	}
	return def
}

func NowUnix() int64 {
	return time.Now().Unix()
}
