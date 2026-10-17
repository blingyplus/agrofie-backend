// Package appconfig serves the public, unauthenticated GET /config document
// the mobile/web clients fetch on start. It is the single place the public
// web domain lives, so it can change without an app release.
package appconfig

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// Version of the response shape. Bump only on breaking changes.
const Version = 1

type Maintenance struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message"`
}

type Config struct {
	Version            int             `json:"version"`
	WebBaseURL         string          `json:"webBaseUrl"`
	InviteURL          string          `json:"inviteUrl"`
	ProfileURLTemplate string          `json:"profileUrlTemplate"`
	SupportEmail       string          `json:"supportEmail"`
	MinAppVersion      string          `json:"minAppVersion"`
	FeatureFlags       map[string]bool `json:"featureFlags"`
	Maintenance        Maintenance     `json:"maintenance"`
}

// FromEnv builds the config from environment variables (getenv is injectable
// for tests). Unset values fall back to sensible defaults.
func FromEnv(getenv func(string) string) Config {
	base := strings.TrimRight(strings.TrimSpace(getenv("WEB_BASE_URL")), "/")
	if base == "" {
		base = "https://agrofie.app" // placeholder until a real domain exists
	}
	invite := strings.TrimSpace(getenv("INVITE_URL"))
	if invite == "" {
		invite = base + "/join"
	}
	tmpl := strings.TrimSpace(getenv("PROFILE_URL_TEMPLATE"))
	if tmpl == "" {
		tmpl = "{webBaseUrl}/t/{id}"
	}
	support := strings.TrimSpace(getenv("SUPPORT_EMAIL"))
	if support == "" {
		support = "support@agrofie.app"
	}
	minV := strings.TrimSpace(getenv("MIN_APP_VERSION"))
	if minV == "" {
		minV = "0.0.0"
	}
	flags := map[string]bool{}
	if raw := strings.TrimSpace(getenv("FEATURE_FLAGS")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &flags); err != nil {
			slog.Warn("FEATURE_FLAGS is not a JSON object of booleans; ignoring", "err", err)
			flags = map[string]bool{}
		}
	}
	return Config{
		Version:            Version,
		WebBaseURL:         base,
		InviteURL:          invite,
		ProfileURLTemplate: strings.ReplaceAll(tmpl, "{webBaseUrl}", base),
		SupportEmail:       support,
		MinAppVersion:      minV,
		FeatureFlags:       flags,
		Maintenance: Maintenance{
			Enabled: strings.EqualFold(strings.TrimSpace(getenv("MAINTENANCE_ENABLED")), "true"),
			Message: strings.TrimSpace(getenv("MAINTENANCE_MESSAGE")),
		},
	}
}

// Handler serves cfg as JSON with a short public cache. CORS is applied by the gateway.
func Handler(cfg Config) http.HandlerFunc {
	body, _ := json.Marshal(cfg)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write(body)
	}
}

// Load reads the process environment.
func Load() Config { return FromEnv(os.Getenv) }
