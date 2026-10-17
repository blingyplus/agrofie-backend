package media

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Media kind codes (rows of media_kinds; codes are immutable).
const (
	KindProfilePhoto         = "profile_photo"
	KindCoverImage           = "cover_image"
	KindVerificationDocument = "verification_document"
	KindGalleryImage         = "gallery_image"
)

const (
	MaxImageBytes = 8 << 20
	MaxPDFBytes   = 10 << 20
	// MaxUploadBytes is the largest body any kind may send (multipart overhead added by the handler).
	MaxUploadBytes = MaxPDFBytes

	VariantOriginal = "original"
	VariantMedium   = "medium"
	VariantThumb    = "thumb"
)

var (
	ErrNotFound     = errors.New("media not found")
	ErrForbidden    = errors.New("not allowed")
	ErrInvalidInput = errors.New("invalid media")
	ErrTooLarge     = errors.New("file too large")
	ErrInUse        = errors.New("media is attached to a verification and cannot be deleted")
	ErrUnknownKind  = errors.New("unknown media kind")
	ErrWrongKind    = errors.New("media has the wrong kind for this use")
)

// Policy is what a kind accepts. Codes without an entry are rejected.
type Policy struct {
	AllowImage bool
	AllowPDF   bool
}

var policies = map[string]Policy{
	KindProfilePhoto:         {AllowImage: true},
	KindCoverImage:           {AllowImage: true},
	KindGalleryImage:         {AllowImage: true},
	KindVerificationDocument: {AllowImage: true, AllowPDF: true},
}

// MaxBytesFor returns the size ceiling for a sniffed mime type.
func MaxBytesFor(mime string) int64 {
	if mime == MimePDF {
		return MaxPDFBytes
	}
	return MaxImageBytes
}

// CheckUpload validates a sniffed mime and size against a kind's policy.
func CheckUpload(kind, mime string, size int64) error {
	p, ok := policies[kind]
	if !ok {
		return ErrUnknownKind
	}
	switch mime {
	case MimeJPEG, MimePNG, MimeWebP:
		if !p.AllowImage {
			return fmt.Errorf("%w: images are not accepted for %s", ErrInvalidInput, kind)
		}
	case MimePDF:
		if !p.AllowPDF {
			return fmt.Errorf("%w: PDFs are only accepted for verification documents", ErrInvalidInput)
		}
	case MimeHEIC:
		return ErrHEIC
	default:
		return ErrUnsupportedType
	}
	if size > MaxBytesFor(mime) {
		return fmt.Errorf("%w: limit is %d MB", ErrTooLarge, MaxBytesFor(mime)>>20)
	}
	return nil
}

// Media is a stored upload.
type Media struct {
	ID         string
	OwnerID    string
	Kind       string
	Mime       string
	SizeBytes  int64
	Width      *int
	Height     *int
	Visibility string
	Status     string
	Variants   []string
	StorageKey string
	CreatedAt  time.Time
}

func (m *Media) Public() bool { return m.Visibility == "public" }

// Viewer identifies who is asking; zero value is anonymous.
type Viewer struct {
	UserID  string
	IsAdmin bool
}

// CanView: public media is for everyone; private only for its owner or an admin.
func (m *Media) CanView(v Viewer) bool {
	if m.Status != "active" {
		return false
	}
	if m.Public() {
		return true
	}
	return v.UserID != "" && (v.IsAdmin || v.UserID == m.OwnerID)
}

type Service struct {
	q       *db.Queries
	store   Storage
	baseURL string
	secret  []byte
}

// NewService. baseURL is PUBLIC_BASE_URL; secret signs short-lived private URLs.
func NewService(conn db.DBTX, store Storage, baseURL string, signingSecret []byte) *Service {
	return &Service{q: db.New(conn), store: store, baseURL: trimSlash(baseURL), secret: signingSecret}
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// Upload validates, processes and stores a file for ownerID. data must
// already be size-limited by the caller (the handler uses MaxBytesReader).
func (s *Service) Upload(ctx context.Context, ownerID, kindCode string, data []byte) (*Media, error) {
	ownerUUID, err := uuid.Parse(ownerID)
	if err != nil {
		return nil, fmt.Errorf("%w: owner", ErrInvalidInput)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty file", ErrInvalidInput)
	}
	mime := SniffMime(data)
	if err := CheckUpload(kindCode, mime, int64(len(data))); err != nil {
		return nil, err
	}
	kind, err := s.q.GetMediaKind(ctx, kindCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnknownKind
	}
	if err != nil {
		return nil, fmt.Errorf("get kind: %w", err)
	}

	var (
		objects  []Variant
		outMime  = mime
		width    *int
		height   *int
		variants = []string{}
	)
	if mime == MimePDF {
		if err := checkPDF(data); err != nil {
			return nil, err
		}
		objects = []Variant{{Name: VariantOriginal, Data: data}}
	} else {
		objects, err = ProcessImage(data)
		if err != nil {
			if errors.Is(err, ErrTooManyPixels) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		outMime = MimeJPEG
		w, h := objects[0].Width, objects[0].Height
		width, height = &w, &h
		for _, o := range objects {
			if o.Name != VariantOriginal {
				variants = append(variants, o.Name)
			}
		}
	}

	keyID := uuid.New().String()
	var stored []string
	cleanup := func() {
		for _, k := range stored {
			_ = s.store.Delete(context.WithoutCancel(ctx), k)
		}
	}
	for _, o := range objects {
		key := keyID + "/" + o.Name
		if _, err := s.store.Put(ctx, key, bytes.NewReader(o.Data)); err != nil {
			cleanup()
			return nil, fmt.Errorf("store %s: %w", o.Name, err)
		}
		stored = append(stored, key)
	}

	sum := sha256.Sum256(data)
	var w32, h32 *int32
	if width != nil {
		a, b := int32(*width), int32(*height)
		w32, h32 = &a, &b
	}
	row, err := s.q.InsertMedia(ctx, db.InsertMediaParams{
		OwnerUserID: pgUUID(ownerUUID), KindID: kind.ID, Mime: outMime,
		SizeBytes: int64(len(objects[0].Data)), Width: w32, Height: h32,
		StorageKey: keyID, Sha256: hex.EncodeToString(sum[:]),
		Visibility: kind.DefaultVisibility, Variants: variants,
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("insert media: %w", err)
	}
	return &Media{
		ID: uuidStr(row.ID), OwnerID: ownerID, Kind: kindCode, Mime: outMime,
		SizeBytes: int64(len(objects[0].Data)), Width: width, Height: height,
		Visibility: kind.DefaultVisibility, Status: "active", Variants: variants,
		StorageKey: keyID, CreatedAt: row.CreatedAt.Time,
	}, nil
}

// Get returns an active media row. Deleted or malformed ids are ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (*Media, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrNotFound
	}
	r, err := s.q.GetMedia(ctx, pgUUID(u))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get media: %w", err)
	}
	if r.Status != "active" {
		return nil, ErrNotFound
	}
	m := &Media{
		ID: uuidStr(r.ID), OwnerID: uuidStr(r.OwnerUserID), Kind: r.KindCode, Mime: r.Mime,
		SizeBytes: r.SizeBytes, Visibility: r.Visibility, Status: r.Status,
		Variants: r.Variants, StorageKey: r.StorageKey, CreatedAt: r.CreatedAt.Time,
	}
	if r.Width != nil {
		w := int(*r.Width)
		m.Width = &w
	}
	if r.Height != nil {
		h := int(*r.Height)
		m.Height = &h
	}
	return m, nil
}

// Owned returns media id if it exists, belongs to userID and has one of the
// allowed kinds; otherwise ErrNotFound (never reveals other users' media) or ErrWrongKind.
func (s *Service) Owned(ctx context.Context, userID, id string, kinds ...string) (*Media, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.OwnerID != userID {
		return nil, ErrNotFound
	}
	for _, k := range kinds {
		if m.Kind == k {
			return m, nil
		}
	}
	return nil, ErrWrongKind
}

// Open resolves a variant ("" or unknown => original for PDFs; images fall back
// to original when the variant does not exist) and opens it from storage.
func (s *Service) Open(ctx context.Context, m *Media, variant string) (io.ReadSeekCloser, int64, string, error) {
	v := VariantOriginal
	for _, have := range m.Variants {
		if have == variant {
			v = variant
		}
	}
	key := m.StorageKey + "/" + v
	size, err := s.store.Stat(ctx, key)
	if err != nil {
		return nil, 0, "", err
	}
	rc, err := s.store.Open(ctx, key)
	if err != nil {
		return nil, 0, "", err
	}
	return rc, size, v, nil
}

// Delete removes the caller's own media. Media backing a verification is kept
// (admins may still need it).
func (s *Service) Delete(ctx context.Context, ownerID, id string) error {
	m, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if m.OwnerID != ownerID {
		return ErrNotFound
	}
	u, _ := uuid.Parse(id)
	inUse, err := s.q.MediaInUseByVerification(ctx, pgUUID(u))
	if err != nil {
		return err
	}
	if inUse {
		return ErrInUse
	}
	if err := s.q.ClearTalentMediaRefs(ctx, pgUUID(u)); err != nil {
		return err
	}
	if _, err := s.q.MarkMediaDeleted(ctx, pgUUID(u)); err != nil {
		return err
	}
	s.removeObjects(ctx, m.StorageKey, m.Variants)
	return nil
}

func (s *Service) removeObjects(ctx context.Context, key string, variants []string) {
	for _, v := range append([]string{VariantOriginal}, variants...) {
		if err := s.store.Delete(context.WithoutCancel(ctx), key+"/"+v); err != nil {
			slog.Warn("media object delete failed", "key", key, "variant", v, "err", err)
		}
	}
}

// AttachTalentPhoto / AttachTalentCover point the caller's talent profile at
// media they own. Returns ErrNotFound if the user has no talent profile.
func (s *Service) AttachTalentPhoto(ctx context.Context, userID string, mediaID *string) error {
	return s.attachTalent(ctx, userID, mediaID, KindProfilePhoto, func(u, m pgtype.UUID) (int64, error) {
		return s.q.SetTalentPhoto(ctx, db.SetTalentPhotoParams{UserID: u, PhotoMediaID: m})
	})
}

func (s *Service) AttachTalentCover(ctx context.Context, userID string, mediaID *string) error {
	return s.attachTalent(ctx, userID, mediaID, KindCoverImage, func(u, m pgtype.UUID) (int64, error) {
		return s.q.SetTalentCover(ctx, db.SetTalentCoverParams{UserID: u, CoverMediaID: m})
	})
}

func (s *Service) attachTalent(ctx context.Context, userID string, mediaID *string, kind string,
	set func(user, media pgtype.UUID) (int64, error)) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return ErrForbidden
	}
	var mid pgtype.UUID // NULL clears
	if mediaID != nil && *mediaID != "" {
		m, err := s.Owned(ctx, userID, *mediaID, kind)
		if err != nil {
			return err
		}
		u, _ := uuid.Parse(m.ID)
		mid = pgUUID(u)
	}
	n, err := set(pgUUID(uid), mid)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// URL builds the absolute URL for a media id. variant "" means original.
func (s *Service) URL(id, variant string) string {
	return URLFor(s.baseURL, id, variant)
}

// URLFor builds /media/{id}[?v=variant] on baseURL.
func URLFor(baseURL, id, variant string) string {
	u := trimSlash(baseURL) + "/media/" + id
	if variant != "" && variant != VariantOriginal {
		u += "?v=" + url.QueryEscape(variant)
	}
	return u
}

// SignedURL returns a short-lived URL that works without an Authorization
// header (so <Image>/browsers can open private media). The caller must have
// already authorized the viewer.
func (s *Service) SignedURL(id, variant string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	sig := s.sign(id, variant, exp)
	u := URLFor(s.baseURL, id, variant)
	sep := "?"
	if variant != "" && variant != VariantOriginal {
		sep = "&"
	}
	return u + sep + "exp=" + strconv.FormatInt(exp, 10) + "&sig=" + sig
}

func (s *Service) sign(id, variant string, exp int64) string {
	if variant == VariantOriginal {
		variant = ""
	}
	mac := hmac.New(sha256.New, s.secret)
	_, _ = fmt.Fprintf(mac, "%s|%s|%d", id, variant, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature checks a signed URL's exp/sig for (id, variant).
func (s *Service) VerifySignature(id, variant, expStr, sig string) bool {
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(s.sign(id, variant, exp)), []byte(sig))
}

// CollectOrphans soft-deletes active media older than cutoff that nothing
// references and removes its files. dryRun only counts.
func (s *Service) CollectOrphans(ctx context.Context, cutoff time.Time, dryRun bool) (int, error) {
	rows, err := s.q.ListOrphanMedia(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if err != nil {
		return 0, err
	}
	if dryRun {
		return len(rows), nil
	}
	for _, r := range rows {
		if _, err := s.q.MarkMediaDeleted(ctx, r.ID); err != nil {
			return 0, err
		}
		s.removeObjects(ctx, r.StorageKey, r.Variants)
	}
	return len(rows), nil
}

// checkPDF is a light sanity check, not a malware scan: header, EOF marker,
// and no obviously active content. Real AV scanning is a later decision.
func checkPDF(b []byte) error {
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		return fmt.Errorf("%w: not a PDF", ErrInvalidInput)
	}
	tail := b
	if len(tail) > 2048 {
		tail = tail[len(tail)-2048:]
	}
	if !bytes.Contains(tail, []byte("%%EOF")) {
		return fmt.Errorf("%w: truncated PDF", ErrInvalidInput)
	}
	for _, bad := range []string{"/JavaScript", "/JS ", "/JS(", "/Launch", "/EmbeddedFile"} {
		if bytes.Contains(b, []byte(bad)) {
			return fmt.Errorf("%w: PDF contains active content", ErrInvalidInput)
		}
	}
	return nil
}

func pgUUID(u uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: u, Valid: true} }
func uuidStr(u pgtype.UUID) string   { return uuid.UUID(u.Bytes).String() }
