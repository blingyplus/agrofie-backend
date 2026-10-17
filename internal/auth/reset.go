package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	resetMinInterval = 30 * time.Second // one code email per address per 30s
	resetWindow      = time.Hour
	resetMaxPerHour  = 5 // code emails per address per hour
	resetMaxAttempts = 5 // wrong codes per issued code
)

var sixDigits = regexp.MustCompile(`^[0-9]{6}$`)

// RequestPasswordReset starts a Kratos recovery (code method) for the email.
// It never reveals whether the account exists: unknown, throttled and
// delivered requests all return nil. Only a malformed address is rejected.
func (s *Service) RequestPasswordReset(ctx context.Context, rawEmail string) error {
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	if _, err := mail.ParseAddress(email); err != nil || !strings.Contains(email, "@") || len(email) > 254 {
		return FriendlyError("Enter a valid email address.")
	}
	now := time.Now()
	windowStart, windowCount := now, int32(1)
	if prev, err := s.queries.GetPasswordResetRequest(ctx, email); err == nil {
		if prev.RequestedAt.Valid && now.Sub(prev.RequestedAt.Time) < resetMinInterval {
			return nil // too soon: silently keep the code already sent
		}
		if prev.WindowStartedAt.Valid && now.Sub(prev.WindowStartedAt.Time) < resetWindow {
			if prev.WindowCount >= resetMaxPerHour {
				return nil
			}
			windowStart, windowCount = prev.WindowStartedAt.Time, prev.WindowCount+1
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	fl, err := s.kratos.StartRecovery(ctx, email)
	if err != nil {
		slog.Error("password reset: kratos recovery failed", "err", err)
		return errors.New("we could not send a code right now, please try again in a minute")
	}
	cookies, _ := json.Marshal(fl.Cookies)
	return s.queries.UpsertPasswordResetRequest(ctx, db.UpsertPasswordResetRequestParams{
		Email: email, FlowID: fl.FlowID, CsrfToken: fl.CSRF, Cookies: cookies,
		WindowStartedAt: pgtype.Timestamptz{Time: windowStart, Valid: true}, WindowCount: windowCount,
	})
}

func codeHash(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:])
}

// ResetPassword redeems the emailed code, sets the new password, and signs the
// account out everywhere. A password the policy rejects can be retried with
// the same code; a wrong code counts against five attempts.
func (s *Service) ResetPassword(ctx context.Context, rawEmail, code, newPassword string) error {
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	code = strings.TrimSpace(code)
	if email == "" || !sixDigits.MatchString(code) {
		return ErrInvalidCode
	}
	if len(newPassword) < 8 {
		return FriendlyError("Your new password must be at least 8 characters long.")
	}
	if len(newPassword) > 128 {
		return FriendlyError("Your new password is too long (128 characters at most).")
	}

	row, err := s.queries.GetPasswordResetRequest(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidCode
		}
		return err
	}
	fl := &recoveryFlow{FlowID: row.FlowID, CSRF: row.CsrfToken, Cookies: map[string]string{}}
	_ = json.Unmarshal(row.Cookies, &fl.Cookies)

	settingsFlow := ""
	if row.SettingsFlowID != nil && row.VerifiedCodeHash != nil && *row.VerifiedCodeHash == codeHash(code) {
		settingsFlow = *row.SettingsFlowID // code already redeemed; this is a retry with a better password
	} else {
		if row.Attempts >= resetMaxAttempts {
			_ = s.queries.DeletePasswordResetRequest(ctx, email)
			return ErrInvalidCode
		}
		sf, err := s.kratos.SubmitRecoveryCode(ctx, fl, code)
		if err != nil {
			if errors.Is(err, ErrInvalidCode) {
				if n, berr := s.queries.BumpPasswordResetAttempts(ctx, email); berr == nil && n >= resetMaxAttempts {
					_ = s.queries.DeletePasswordResetRequest(ctx, email)
				}
			}
			return err
		}
		settingsFlow = sf
		cookies, _ := json.Marshal(fl.Cookies)
		if err := s.queries.MarkPasswordResetVerified(ctx, db.MarkPasswordResetVerifiedParams{
			Email: email, SettingsFlowID: sf, VerifiedCodeHash: codeHash(code), Cookies: cookies,
		}); err != nil {
			return err
		}
	}

	identityID, err := s.kratos.SetRecoveredPassword(ctx, fl, settingsFlow, newPassword)
	if err != nil {
		var friendly FriendlyError
		if !errors.As(err, &friendly) {
			_ = s.queries.DeletePasswordResetRequest(ctx, email)
		}
		return err
	}
	if err := s.kratos.RevokeAllSessions(ctx, identityID); err != nil {
		slog.Error("password reset: revoke sessions failed", "identity", identityID, "err", err)
		return errors.New("your password was changed but we could not sign out your other devices, please contact support")
	}
	_ = s.queries.DeletePasswordResetRequest(ctx, email)
	return nil
}
