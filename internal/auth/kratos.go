package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Kratos talks to Ory Kratos public + admin APIs.
type Kratos struct {
	adminURL  string
	publicURL string
	client    *http.Client
}

func NewKratos(adminURL, publicURL string) *Kratos {
	return &Kratos{
		adminURL:  strings.TrimRight(adminURL, "/"),
		publicURL: strings.TrimRight(publicURL, "/"),
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

type identityTraits struct {
	Email       string  `json:"email"`
	Phone       *string `json:"phone,omitempty"`
	DisplayName string  `json:"display_name"`
}

type createIdentityBody struct {
	SchemaID            string           `json:"schema_id"`
	Traits              identityTraits   `json:"traits"`
	Credentials         map[string]any   `json:"credentials,omitempty"`
	VerifiableAddresses []map[string]any `json:"verifiable_addresses,omitempty"`
}

type identityResponse struct {
	ID string `json:"id"`
}

type sessionResponse struct {
	ID           string `json:"id"`
	SessionToken string `json:"session_token"`
	Identity     *struct {
		ID     string         `json:"id"`
		Traits identityTraits `json:"traits"`
	} `json:"identity"`
	Session *struct {
		ID       string `json:"id"`
		Identity *struct {
			ID     string         `json:"id"`
			Traits identityTraits `json:"traits"`
		} `json:"identity"`
	} `json:"session"`
}

func (s sessionResponse) identityID() string {
	if s.Identity != nil && s.Identity.ID != "" {
		return s.Identity.ID
	}
	if s.Session != nil && s.Session.Identity != nil {
		return s.Session.Identity.ID
	}
	return ""
}

func (s sessionResponse) sessionID() string {
	if s.ID != "" {
		return s.ID
	}
	if s.Session != nil {
		return s.Session.ID
	}
	return ""
}


type loginFlowResponse struct {
	ID string `json:"id"`
}

type kratosErrorBody struct {
	Error *struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
	} `json:"error"`
	UI *struct {
		Messages []struct {
			Text string `json:"text"`
			Type string `json:"type"`
		} `json:"messages"`
	} `json:"ui"`
}

// CreateIdentityWithPassword creates a Kratos identity and returns its id.
func (k *Kratos) CreateIdentityWithPassword(ctx context.Context, email, phone, displayName, password string) (string, error) {
	body := createIdentityBody{
		SchemaID: "default",
		Traits: identityTraits{
			Email:       email,
			DisplayName: displayName,
		},
		Credentials: map[string]any{
			"password": map[string]any{
				"config": map[string]any{
					"password": password,
				},
			},
		},
		VerifiableAddresses: []map[string]any{
			{
				"value":    email,
				"via":      "email",
				"verified": true,
				"status":   "completed",
			},
		},
	}
	if phone != "" {
		p := phone
		body.Traits.Phone = &p
	}

	var out identityResponse
	if err := k.doJSON(ctx, http.MethodPost, k.adminURL+"/admin/identities", body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("kratos: empty identity id")
	}
	return out.ID, nil
}

// LoginWithPassword runs the native API login flow.
func (k *Kratos) LoginWithPassword(ctx context.Context, identifier, password string) (sessionToken string, identityID string, err error) {
	var flow loginFlowResponse
	if err := k.doJSON(ctx, http.MethodGet, k.publicURL+"/self-service/login/api", nil, &flow); err != nil {
		return "", "", err
	}
	if flow.ID == "" {
		return "", "", fmt.Errorf("kratos: empty login flow id")
	}

	payload := map[string]any{
		"method":     "password",
		"identifier": identifier,
		"password":   password,
	}
	url := k.publicURL + "/self-service/login?flow=" + flow.ID
	raw, status, err := k.doJSONRaw(ctx, http.MethodPost, url, payload)
	if err != nil {
		return "", "", err
	}
	if status >= 300 {
		return "", "", fmt.Errorf("%s", formatError(status, raw))
	}
	var out sessionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", err
	}
	identityID = out.identityID()
	if out.SessionToken == "" || identityID == "" {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 400 {
			snippet = snippet[:400]
		}
		return "", "", fmt.Errorf("kratos: login did not return session (status %d): %s", status, snippet)
	}
	return out.SessionToken, identityID, nil
}

// WhoAmI validates a session token and returns the Kratos identity id.
func (k *Kratos) WhoAmI(ctx context.Context, sessionToken string) (identityID string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.publicURL+"/sessions/whoami", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Session-Token", sessionToken)
	req.Header.Set("Accept", "application/json")

	res, err := k.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("kratos whoami: %s", formatError(res.StatusCode, body))
	}
	var out sessionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	id := out.identityID()
	if id == "" {
		return "", fmt.Errorf("kratos: whoami missing identity")
	}
	return id, nil
}

// DisableSession revokes a session by token (admin lookup via whoami then disable).
func (k *Kratos) DisableSession(ctx context.Context, sessionToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.publicURL+"/sessions/whoami", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Session-Token", sessionToken)
	req.Header.Set("Accept", "application/json")
	res, err := k.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode == http.StatusUnauthorized {
		return nil
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("kratos whoami for logout: %s", formatError(res.StatusCode, body))
	}
	var out sessionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return err
	}
	sid := out.sessionID()
	if sid == "" {
		return nil
	}
	delReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, k.adminURL+"/admin/sessions/"+sid, nil)
	if err != nil {
		return err
	}
	delRes, err := k.client.Do(delReq)
	if err != nil {
		return err
	}
	defer delRes.Body.Close()
	if delRes.StatusCode >= 300 && delRes.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(delRes.Body)
		return fmt.Errorf("kratos disable session: %s", formatError(delRes.StatusCode, b))
	}
	return nil
}

// DeleteIdentity removes a Kratos identity (compensation on failed provisioning).
func (k *Kratos) DeleteIdentity(ctx context.Context, identityID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, k.adminURL+"/admin/identities/"+identityID, nil)
	if err != nil {
		return err
	}
	res, err := k.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("kratos delete identity: %s", formatError(res.StatusCode, b))
	}
	return nil
}

func (k *Kratos) doJSON(ctx context.Context, method, url string, payload any, out any) error {
	raw, status, err := k.doJSONRaw(ctx, method, url, payload)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("%s", formatError(status, raw))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (k *Kratos) doJSONRaw(ctx context.Context, method, url string, payload any) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := k.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, res.StatusCode, err
	}
	return raw, res.StatusCode, nil
}

func formatError(status int, body []byte) string {
	var errBody kratosErrorBody
	if json.Unmarshal(body, &errBody) == nil {
		if errBody.Error != nil && errBody.Error.Message != "" {
			msg := errBody.Error.Message
			if errBody.Error.Reason != "" {
				msg += ": " + errBody.Error.Reason
			}
			return fmt.Sprintf("status %d: %s", status, msg)
		}
		if errBody.UI != nil {
			for _, m := range errBody.UI.Messages {
				if m.Type == "error" && m.Text != "" {
					return fmt.Sprintf("status %d: %s", status, m.Text)
				}
			}
		}
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return fmt.Sprintf("status %d", status)
	}
	if len(trimmed) > 300 {
		trimmed = trimmed[:300]
	}
	return fmt.Sprintf("status %d: %s", status, trimmed)
}
