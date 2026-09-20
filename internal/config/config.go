package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	ServiceName string
	HTTPPort    int
	DatabaseURL string
	RedisURL    string
	AuthURL     string
	BookingURL  string
	PaymentsURL string
}

func Load(serviceName string) Config {
	return Config{
		ServiceName: serviceName,
		HTTPPort:    envInt("HTTP_PORT", 8080),
		DatabaseURL: env("DATABASE_URL", "postgres://agrofie:agrofie@localhost:5432/agrofie?sslmode=disable"),
		RedisURL:    env("REDIS_URL", "redis://localhost:6379/0"),
		AuthURL:     env("AUTH_URL", "http://localhost:8081"),
		BookingURL:  env("BOOKING_URL", "http://localhost:8082"),
		PaymentsURL: env("PAYMENTS_URL", "http://localhost:8083"),
	}
}

func (c Config) Addr() string {
	return fmt.Sprintf(":%d", c.HTTPPort)
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
