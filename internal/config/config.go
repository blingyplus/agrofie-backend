package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	ServiceName       string
	HTTPPort          int
	DatabaseURL       string
	RedisURL          string
	AuthURL           string
	BookingURL        string
	PaymentsURL       string
	KratosAdminURL    string
	KratosPublicURL   string
	AdminEmail        string
	AdminPassword     string
	AdminDisplay      string
	PaystackSecretKey string
	CommissionPercent float64
	PublicBaseURL     string
	// PaymentsProvider: "fake" forces the test provider even if a Paystack key is set.
	PaymentsProvider string
	// DevAllowEarlyComplete lets a booking be completed before its event ends.
	// Local testing only; never set in production.
	DevAllowEarlyComplete bool
	// MediaDir is the local-disk root for uploads (default ./data/media).
	MediaDir string
	// MediaSigningSecret signs short-lived private-media URLs. If empty a random
	// per-process secret is used (links die on restart; set it in any multi-instance deploy).
	MediaSigningSecret string
}

func Load(serviceName string) Config {
	return Config{
		ServiceName:           serviceName,
		HTTPPort:              envInt("HTTP_PORT", 8080),
		DatabaseURL:           env("DATABASE_URL", "postgres://agrofie:agrofie@localhost:5432/agrofie?sslmode=disable"),
		RedisURL:              env("REDIS_URL", "redis://localhost:6379/0"),
		AuthURL:               env("AUTH_URL", "http://localhost:8081"),
		BookingURL:            env("BOOKING_URL", "http://localhost:8082"),
		PaymentsURL:           env("PAYMENTS_URL", "http://localhost:8083"),
		KratosAdminURL:        env("KRATOS_ADMIN_URL", "http://localhost:4434"),
		KratosPublicURL:       env("KRATOS_PUBLIC_URL", "http://localhost:4433"),
		AdminEmail:            env("ADMIN_EMAIL", "admin@agrofie.local"),
		AdminPassword:         env("ADMIN_PASSWORD", ""),
		AdminDisplay:          env("ADMIN_DISPLAY_NAME", "Agrofie Admin"),
		PaystackSecretKey:     env("PAYSTACK_SECRET_KEY", ""),
		CommissionPercent:     envFloat("COMMISSION_PERCENT", 12.5),
		PaymentsProvider:      env("PAYMENTS_PROVIDER", ""),
		PublicBaseURL:         env("PUBLIC_BASE_URL", "http://localhost:8080"),
		DevAllowEarlyComplete: env("DEV_ALLOW_EARLY_COMPLETE", "") == "true",
		MediaDir:              env("MEDIA_DIR", "./data/media"),
		MediaSigningSecret:    env("MEDIA_SIGNING_SECRET", ""),
	}
}

func (c Config) Addr() string {
	return fmt.Sprintf(":%d", c.HTTPPort)
}

func envFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
