package appconfig

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaultsDeriveFromBase(t *testing.T) {
	c := FromEnv(env(map[string]string{"WEB_BASE_URL": "https://example.gh/"}))
	if c.WebBaseURL != "https://example.gh" || c.InviteURL != "https://example.gh/join" ||
		c.ProfileURLTemplate != "https://example.gh/t/{id}" || c.Version != 1 || c.MinAppVersion != "0.0.0" {
		t.Fatalf("unexpected: %+v", c)
	}
}

func TestOverridesAndFlags(t *testing.T) {
	c := FromEnv(env(map[string]string{
		"WEB_BASE_URL": "https://a.gh", "INVITE_URL": "https://a.gh/get", "FEATURE_FLAGS": `{"socialLogin":false}`,
		"MAINTENANCE_ENABLED": "true", "MAINTENANCE_MESSAGE": "Back soon", "MIN_APP_VERSION": "1.2.0",
	}))
	if c.InviteURL != "https://a.gh/get" || c.FeatureFlags["socialLogin"] || !c.Maintenance.Enabled ||
		c.Maintenance.Message != "Back soon" || c.MinAppVersion != "1.2.0" {
		t.Fatalf("unexpected: %+v", c)
	}
	if bad := FromEnv(env(map[string]string{"FEATURE_FLAGS": "nope"})); bad.FeatureFlags == nil || len(bad.FeatureFlags) != 0 {
		t.Fatal("bad flags should yield empty map")
	}
}

func TestHandler(t *testing.T) {
	rr := httptest.NewRecorder()
	Handler(FromEnv(env(nil)))(rr, httptest.NewRequest("GET", "/config", nil))
	if rr.Header().Get("Cache-Control") == "" {
		t.Fatal("missing cache-control")
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil || out["webBaseUrl"] == nil || out["featureFlags"] == nil {
		t.Fatalf("bad body: %s", rr.Body)
	}
}
