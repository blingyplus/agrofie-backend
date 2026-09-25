package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blingyplus/agrofie-backend/internal/auth"
	"github.com/blingyplus/agrofie-backend/internal/domain"
	"github.com/google/uuid"
)

func TestRegisterRejectsAdminRole(t *testing.T) {
	svc := auth.NewService(nil, auth.NewKratos("http://unused", "http://unused"))
	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:       "a@example.com",
		Password:    "password12",
		DisplayName: "Admin Wannabe",
		RoleCode:    domain.RoleAdmin,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err != auth.ErrForbiddenRole {
		t.Fatalf("got %v, want ErrForbiddenRole", err)
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	svc := auth.NewService(nil, auth.NewKratos("http://unused", "http://unused"))
	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:       "a@example.com",
		Password:    "short",
		DisplayName: "Talent",
		RoleCode:    domain.RoleTalent,
	})
	if err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("got %v, want password validation error", err)
	}
}

func TestKratosLoginFlow(t *testing.T) {
	identityID := uuid.NewString()
	sessionToken := "st_" + uuid.NewString()

	mux := http.NewServeMux()
	mux.HandleFunc("/self-service/login/api", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "flow-1"})
	})
	mux.HandleFunc("/self-service/login", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_token": sessionToken,
			"identity":      map[string]any{"id": identityID},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	k := auth.NewKratos(srv.URL, srv.URL)
	token, id, err := k.LoginWithPassword(context.Background(), "user@example.com", "password12")
	if err != nil {
		t.Fatal(err)
	}
	if token != sessionToken || id != identityID {
		t.Fatalf("token=%q id=%q", token, id)
	}
}

func TestKratosWhoAmI(t *testing.T) {
	identityID := uuid.NewString()
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions/whoami", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Session-Token") != "good" {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "sess-1",
			"identity": map[string]any{"id": identityID},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	k := auth.NewKratos(srv.URL, srv.URL)
	id, err := k.WhoAmI(context.Background(), "good")
	if err != nil {
		t.Fatal(err)
	}
	if id != identityID {
		t.Fatalf("got %s", id)
	}
	if _, err := k.WhoAmI(context.Background(), "bad"); err == nil {
		t.Fatal("expected error for bad token")
	}
}
