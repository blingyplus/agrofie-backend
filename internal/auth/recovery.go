package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Kratos' native (API) recovery flow cannot hand a session back to a caller, so
// the code is redeemed through a browser-type flow that this service drives
// server-side with its own tiny cookie store. Neither the cookies nor Kratos
// itself are ever exposed to the client.

// recoveryFlow is the persisted state of an in-progress recovery.
type recoveryFlow struct {
	FlowID  string
	CSRF    string
	Cookies map[string]string
}

// ErrInvalidCode: wrong, expired, or already-used recovery code (or no flow).
var ErrInvalidCode = errors.New("that code is wrong or has expired, please request a new one")

// FriendlyError is shown to the user as-is (e.g. a password policy message).
type FriendlyError string

func (e FriendlyError) Error() string { return string(e) }

type flowDoc struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Identity *struct {
		ID string `json:"id"`
	} `json:"identity"`
	UI struct {
		Nodes []struct {
			Attributes struct {
				Name  string `json:"name"`
				Value any    `json:"value"`
			} `json:"attributes"`
			Messages []struct {
				Text string `json:"text"`
				Type string `json:"type"`
			} `json:"messages"`
		} `json:"nodes"`
		Messages []struct {
			Text string `json:"text"`
			Type string `json:"type"`
		} `json:"messages"`
	} `json:"ui"`
	RedirectBrowserTo string `json:"redirect_browser_to"`
}

func (f flowDoc) csrf() string {
	for _, n := range f.UI.Nodes {
		if n.Attributes.Name == "csrf_token" {
			if s, ok := n.Attributes.Value.(string); ok {
				return s
			}
		}
	}
	return ""
}

// errorTexts collects every user-facing error message in the flow.
func (f flowDoc) errorTexts() []string {
	var out []string
	for _, m := range f.UI.Messages {
		if m.Type == "error" && m.Text != "" {
			out = append(out, m.Text)
		}
	}
	for _, n := range f.UI.Nodes {
		for _, m := range n.Messages {
			if m.Type == "error" && m.Text != "" {
				out = append(out, m.Text)
			}
		}
	}
	return out
}

// cookieDo performs a request carrying/collecting cookies, never following redirects.
func (k *Kratos) cookieDo(ctx context.Context, method, rawURL string, cookies map[string]string, payload any) ([]byte, int, map[string]string, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for n, v := range cookies {
		req.AddCookie(&http.Cookie{Name: n, Value: v})
	}
	c := *k.client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := c.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, res.StatusCode, nil, err
	}
	merged := map[string]string{}
	for n, v := range cookies {
		merged[n] = v
	}
	for _, ck := range res.Cookies() {
		if ck.MaxAge < 0 || ck.Value == "" {
			delete(merged, ck.Name)
		} else {
			merged[ck.Name] = ck.Value
		}
	}
	return raw, res.StatusCode, merged, nil
}

// StartRecovery creates a recovery flow and asks Kratos to email a code to
// `email`. Kratos answers identically whether or not the account exists.
func (k *Kratos) StartRecovery(ctx context.Context, email string) (*recoveryFlow, error) {
	raw, status, cookies, err := k.cookieDo(ctx, http.MethodGet, k.publicURL+"/self-service/recovery/browser", nil, nil)
	if err != nil {
		return nil, err
	}
	var fd flowDoc
	if status >= 300 || json.Unmarshal(raw, &fd) != nil || fd.ID == "" {
		return nil, fmt.Errorf("kratos recovery init: %s", formatError(status, raw))
	}
	fl := &recoveryFlow{FlowID: fd.ID, CSRF: fd.csrf(), Cookies: cookies}

	raw, status, cookies, err = k.cookieDo(ctx, http.MethodPost, k.publicURL+"/self-service/recovery?flow="+url.QueryEscape(fl.FlowID), fl.Cookies,
		map[string]string{"method": "code", "email": email, "csrf_token": fl.CSRF})
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("kratos recovery submit: %s", formatError(status, raw))
	}
	fl.Cookies = cookies
	return fl, nil
}

// SubmitRecoveryCode redeems the code. On success Kratos has created a
// short-lived recovery session (kept in the returned cookies) and a settings
// flow whose id is returned.
func (k *Kratos) SubmitRecoveryCode(ctx context.Context, fl *recoveryFlow, code string) (settingsFlowID string, err error) {
	raw, status, cookies, err := k.cookieDo(ctx, http.MethodPost, k.publicURL+"/self-service/recovery?flow="+url.QueryEscape(fl.FlowID), fl.Cookies,
		map[string]string{"method": "code", "code": code, "csrf_token": fl.CSRF})
	if err != nil {
		return "", err
	}
	if status >= 500 {
		return "", fmt.Errorf("kratos recovery code: %s", formatError(status, raw))
	}
	var fd flowDoc
	_ = json.Unmarshal(raw, &fd)
	if status == http.StatusUnprocessableEntity && fd.RedirectBrowserTo != "" {
		if u, perr := url.Parse(fd.RedirectBrowserTo); perr == nil && u.Query().Get("flow") != "" {
			fl.Cookies = cookies
			return u.Query().Get("flow"), nil
		}
	}
	return "", ErrInvalidCode
}

// SetRecoveredPassword sets the new password on the settings flow opened by a
// redeemed code and returns the identity id. A password the policy rejects is a
// FriendlyError (the flow stays usable); any other failure is ErrInvalidCode.
func (k *Kratos) SetRecoveredPassword(ctx context.Context, fl *recoveryFlow, settingsFlowID, newPassword string) (identityID string, err error) {
	raw, status, cookies, err := k.cookieDo(ctx, http.MethodGet, k.publicURL+"/self-service/settings/flows?id="+url.QueryEscape(settingsFlowID), fl.Cookies, nil)
	if err != nil {
		return "", err
	}
	var fd flowDoc
	if status >= 500 {
		return "", fmt.Errorf("kratos settings flow: %s", formatError(status, raw))
	}
	if status >= 300 || json.Unmarshal(raw, &fd) != nil || fd.Identity == nil {
		return "", ErrInvalidCode
	}
	identityID = fd.Identity.ID
	raw, status, _, err = k.cookieDo(ctx, http.MethodPost, k.publicURL+"/self-service/settings?flow="+url.QueryEscape(settingsFlowID), cookies,
		map[string]string{"method": "password", "password": newPassword, "csrf_token": fd.csrf()})
	if err != nil {
		return "", err
	}
	if status >= 500 {
		return "", fmt.Errorf("kratos settings submit: %s", formatError(status, raw))
	}
	var res flowDoc
	_ = json.Unmarshal(raw, &res)
	if status < 300 && res.State == "success" {
		return identityID, nil
	}
	if status == http.StatusBadRequest {
		if msgs := res.errorTexts(); len(msgs) > 0 {
			return "", FriendlyError(strings.TrimSpace(msgs[0]))
		}
	}
	return "", ErrInvalidCode
}

// RevokeAllSessions signs the identity out everywhere (including the
// short-lived recovery session).
func (k *Kratos) RevokeAllSessions(ctx context.Context, identityID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, k.adminURL+"/admin/identities/"+identityID+"/sessions", nil)
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
		return fmt.Errorf("kratos revoke sessions: %s", formatError(res.StatusCode, b))
	}
	return nil
}
