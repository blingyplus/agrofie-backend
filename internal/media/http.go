package media

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/blingyplus/agrofie-backend/graph/gqlauth"
)

// SignedURLTTL is how long a private-media link stays valid.
const SignedURLTTL = 10 * time.Minute

// multipartOverhead is allowed on top of the largest file for boundaries and the kind field.
const multipartOverhead = 1 << 20

// Handler serves POST /media, GET /media/{id} and DELETE /media/{id}.
// Wrap it with gqlauth.Middleware so a Bearer token resolves to a principal.
func Handler(svc *Service) http.Handler {
	h := &handler{svc: svc, limiter: newLimiter(30, 10*time.Minute)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /media", h.upload)
	mux.HandleFunc("GET /media/{id}", h.get)
	mux.HandleFunc("HEAD /media/{id}", h.get)
	mux.HandleFunc("DELETE /media/{id}", h.delete)
	return mux
}

type handler struct {
	svc     *Service
	limiter *limiter
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *handler) viewer(r *http.Request) Viewer {
	if p, ok := gqlauth.FromContext(r.Context()); ok {
		return Viewer{UserID: p.UserID, IsAdmin: p.HasRole("admin")}
	}
	return Viewer{}
}

func (h *handler) upload(w http.ResponseWriter, r *http.Request) {
	p, ok := gqlauth.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "sign in to upload")
		return
	}
	if !h.limiter.allow(p.UserID) {
		w.Header().Set("Retry-After", "60")
		writeErr(w, http.StatusTooManyRequests, "too many uploads, try again in a few minutes")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes+multipartOverhead)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, "file too large")
			return
		}
		writeErr(w, http.StatusBadRequest, "expected multipart form with 'file' and 'kind'")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	kind := r.FormValue("kind")
	fhs := r.MultipartForm.File["file"]
	if kind == "" || len(fhs) != 1 {
		writeErr(w, http.StatusBadRequest, "send exactly one 'file' and a 'kind'")
		return
	}
	data, err := readFile(fhs[0])
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}
	m, err := h.svc.Upload(r.Context(), p.UserID, kind, data)
	if err != nil {
		status, msg := mapErr(err)
		if status >= 500 {
			slog.Error("media upload failed", "err", err)
		}
		writeErr(w, status, msg)
		return
	}
	writeJSON(w, http.StatusCreated, h.svc.Response(m))
}

func readFile(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxUploadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > MaxUploadBytes {
		return nil, ErrTooLarge
	}
	return b, nil
}

func mapErr(err error) (int, string) {
	switch {
	case errors.Is(err, ErrTooLarge):
		return http.StatusRequestEntityTooLarge, err.Error()
	case errors.Is(err, ErrUnsupportedType), errors.Is(err, ErrHEIC):
		return http.StatusUnsupportedMediaType, err.Error()
	case errors.Is(err, ErrUnknownKind), errors.Is(err, ErrInvalidInput), errors.Is(err, ErrTooManyPixels):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, "not found"
	case errors.Is(err, ErrInUse):
		return http.StatusConflict, err.Error()
	default:
		return http.StatusInternalServerError, "upload failed"
	}
}

// UploadResponse is the JSON returned by POST /media.
type UploadResponse struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Mime       string            `json:"mime"`
	Visibility string            `json:"visibility"`
	Width      *int              `json:"width,omitempty"`
	Height     *int              `json:"height,omitempty"`
	URL        string            `json:"url"`
	Variants   map[string]string `json:"variants"`
}

// Response builds the client-facing description. Private media gets signed,
// expiring URLs (the uploader is the owner).
func (s *Service) Response(m *Media) UploadResponse {
	link := func(v string) string {
		if m.Public() {
			return s.URL(m.ID, v)
		}
		return s.SignedURL(m.ID, v, SignedURLTTL)
	}
	vars := map[string]string{VariantOriginal: link(VariantOriginal)}
	for _, v := range m.Variants {
		vars[v] = link(v)
	}
	u := vars[VariantMedium]
	if u == "" {
		u = vars[VariantOriginal]
	}
	return UploadResponse{ID: m.ID, Kind: m.Kind, Mime: m.Mime, Visibility: m.Visibility,
		Width: m.Width, Height: m.Height, URL: u, Variants: vars}
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	variant := r.URL.Query().Get("v")
	m, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if !m.Public() {
		v := h.viewer(r)
		signed := h.svc.VerifySignature(id, variant, r.URL.Query().Get("exp"), r.URL.Query().Get("sig"))
		if !signed && !m.CanView(v) {
			if v.UserID == "" {
				writeErr(w, http.StatusUnauthorized, "sign in required")
			} else {
				writeErr(w, http.StatusNotFound, "not found") // do not reveal that it exists
			}
			return
		}
	}
	rc, size, _, err := h.svc.Open(r.Context(), m, variant)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	defer rc.Close()
	hd := w.Header()
	hd.Set("Content-Type", m.Mime)
	hd.Set("Content-Length", strconv.FormatInt(size, 10))
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	hd.Set("Cross-Origin-Resource-Policy", "cross-origin")
	if m.Public() {
		// Media ids are immutable (a replaced photo gets a new id), so cache forever.
		hd.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		hd.Set("Cache-Control", "private, no-store")
	}
	http.ServeContent(w, r, "", m.CreatedAt, rc)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	p, ok := gqlauth.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if err := h.svc.Delete(r.Context(), p.UserID, r.PathValue("id")); err != nil {
		status, msg := mapErr(err)
		writeErr(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// limiter is a tiny per-key sliding-window rate limiter (in-memory, per process).
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *limiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
