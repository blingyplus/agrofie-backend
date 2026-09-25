package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/blingyplus/agrofie-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbiddenRole      = errors.New("role not allowed for self-registration")
	ErrConflict           = errors.New("user already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// Service provisions marketplace users and sessions via Kratos.
type Service struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	kratos  *Kratos
}

func NewService(pool *pgxpool.Pool, kratos *Kratos) *Service {
	return &Service{
		pool:    pool,
		queries: db.New(pool),
		kratos:  kratos,
	}
}

// User is the marketplace identity returned to API layers.
type User struct {
	ID          string
	Email       string
	Phone       string
	DisplayName string
	RoleCodes   []string
}

// Session pairs an opaque Kratos session token with a marketplace user.
type Session struct {
	Token string
	User  User
}

type RegisterInput struct {
	Email       string
	Phone       string
	Password    string
	DisplayName string
	RoleCode    string
}

type LoginInput struct {
	Identifier string
	Password   string
}

type userRow struct {
	ID    pgtype.UUID
	Email *string
	Phone *string
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (*Session, error) {
	email := strings.TrimSpace(strings.ToLower(in.Email))
	phone := strings.TrimSpace(in.Phone)
	display := strings.TrimSpace(in.DisplayName)
	role := strings.TrimSpace(strings.ToLower(in.RoleCode))
	password := in.Password

	if email == "" || display == "" || password == "" || role == "" {
		return nil, fmt.Errorf("%w: email, display name, password, and role are required", ErrInvalidInput)
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("%w: password must be at least 8 characters", ErrInvalidInput)
	}
	if role != domain.RoleTalent && role != domain.RoleOrganizer {
		return nil, ErrForbiddenRole
	}

	if _, err := s.queries.GetUserByEmail(ctx, &email); err == nil {
		return nil, ErrConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if phone != "" {
		if _, err := s.queries.GetUserByPhone(ctx, &phone); err == nil {
			return nil, ErrConflict
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}

	identityID, err := s.kratos.CreateIdentityWithPassword(ctx, email, phone, display, password)
	if err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "already") || strings.Contains(err.Error(), "409") {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("create kratos identity: %w", err)
	}

	user, err := s.provisionMarketplaceUser(ctx, identityID, email, phone, display, role)
	if err != nil {
		_ = s.kratos.DeleteIdentity(ctx, identityID)
		return nil, err
	}

	// Prefer the native login flow so we receive a session_token.
	token, _, err := s.kratos.LoginWithPassword(ctx, email, password)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &Session{Token: token, User: *user}, nil
}

func (s *Service) Login(ctx context.Context, in LoginInput) (*Session, error) {
	identifier := strings.TrimSpace(in.Identifier)
	if identifier == "" || in.Password == "" {
		return nil, fmt.Errorf("%w: identifier and password are required", ErrInvalidInput)
	}
	if strings.Contains(identifier, "@") {
		identifier = strings.ToLower(identifier)
	}

	token, identityID, err := s.kratos.LoginWithPassword(ctx, identifier, in.Password)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	kid, err := parseUUID(identityID)
	if err != nil {
		return nil, err
	}
	row, err := s.queries.GetUserByKratosIdentityID(ctx, kid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	user, err := s.toUser(ctx, userRow{ID: row.ID, Email: row.Email, Phone: row.Phone})
	if err != nil {
		return nil, err
	}
	return &Session{Token: token, User: *user}, nil
}

func (s *Service) Logout(ctx context.Context, sessionToken string) error {
	if strings.TrimSpace(sessionToken) == "" {
		return nil
	}
	return s.kratos.DisableSession(ctx, sessionToken)
}

func (s *Service) WhoAmI(ctx context.Context, sessionToken string) (*User, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return nil, ErrUnauthorized
	}
	identityID, err := s.kratos.WhoAmI(ctx, sessionToken)
	if err != nil {
		return nil, ErrUnauthorized
	}
	kid, err := parseUUID(identityID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	row, err := s.queries.GetUserByKratosIdentityID(ctx, kid)
	if err != nil {
		return nil, ErrUnauthorized
	}
	return s.toUser(ctx, userRow{ID: row.ID, Email: row.Email, Phone: row.Phone})
}

// SeedAdmin creates an admin user if one with the given email does not exist.
func (s *Service) SeedAdmin(ctx context.Context, email, password, displayName string) (*User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	displayName = strings.TrimSpace(displayName)
	if email == "" || password == "" || displayName == "" {
		return nil, fmt.Errorf("%w: admin email, password, and display name required", ErrInvalidInput)
	}
	if existing, err := s.queries.GetUserByEmail(ctx, &email); err == nil {
		return s.toUser(ctx, userRow{ID: existing.ID, Email: existing.Email, Phone: existing.Phone})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	identityID, err := s.kratos.CreateIdentityWithPassword(ctx, email, "", displayName, password)
	if err != nil {
		return nil, fmt.Errorf("create admin identity: %w", err)
	}
	user, err := s.provisionMarketplaceUser(ctx, identityID, email, "", displayName, domain.RoleAdmin)
	if err != nil {
		_ = s.kratos.DeleteIdentity(ctx, identityID)
		return nil, err
	}
	return user, nil
}

func (s *Service) provisionMarketplaceUser(ctx context.Context, identityID, email, phone, display, roleCode string) (*User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)

	statusID, err := q.GetUserStatusIDByCode(ctx, "active")
	if err != nil {
		return nil, fmt.Errorf("active status: %w", err)
	}
	roleID, err := q.GetRoleIDByCode(ctx, roleCode)
	if err != nil {
		return nil, fmt.Errorf("role %s: %w", roleCode, err)
	}
	kid, err := parseUUID(identityID)
	if err != nil {
		return nil, err
	}

	var phonePtr *string
	if phone != "" {
		phonePtr = &phone
	}
	row, err := q.InsertUser(ctx, db.InsertUserParams{
		Email:            &email,
		Phone:            phonePtr,
		UserStatusID:     statusID,
		KratosIdentityID: kid,
	})
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	if _, err := q.InsertProfile(ctx, db.InsertProfileParams{
		UserID:      row.ID,
		DisplayName: display,
	}); err != nil {
		return nil, fmt.Errorf("insert profile: %w", err)
	}
	if err := q.InsertUserRole(ctx, db.InsertUserRoleParams{
		UserID: row.ID,
		RoleID: roleID,
	}); err != nil {
		return nil, fmt.Errorf("insert role: %w", err)
	}

	switch roleCode {
	case domain.RoleTalent:
		if _, err := q.InsertTalentProfile(ctx, row.ID); err != nil {
			return nil, fmt.Errorf("insert talent profile: %w", err)
		}
	case domain.RoleOrganizer:
		if _, err := q.InsertOrganizerProfile(ctx, row.ID); err != nil {
			return nil, fmt.Errorf("insert organizer profile: %w", err)
		}
	case domain.RoleAdmin:
		// admin has no talent/organizer profile row
	default:
		return nil, fmt.Errorf("%w: %s", ErrForbiddenRole, roleCode)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.toUser(ctx, userRow{ID: row.ID, Email: row.Email, Phone: row.Phone})
}

func (s *Service) toUser(ctx context.Context, row userRow) (*User, error) {
	profile, err := s.queries.GetProfileByUserID(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	roles, err := s.queries.ListRoleCodesForUser(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	u := &User{
		ID:          uuidString(row.ID),
		DisplayName: profile.DisplayName,
		RoleCodes:   roles,
	}
	if row.Email != nil {
		u.Email = *row.Email
	}
	if row.Phone != nil {
		u.Phone = *row.Phone
	}
	return u, nil
}

func parseUUID(s string) (pgtype.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid uuid %q: %w", s, err)
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}
