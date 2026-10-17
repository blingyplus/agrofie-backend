package media_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/blingyplus/agrofie-backend/internal/media"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed media tests")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

func newUser(t *testing.T, tx pgx.Tx, talent bool) string {
	t.Helper()
	var id string
	err := tx.QueryRow(context.Background(), `
		INSERT INTO users (email, user_status_id, kratos_identity_id)
		SELECT 'm_' || gen_random_uuid() || '@example.test', id, gen_random_uuid() FROM user_statuses WHERE code = 'active'
		RETURNING id::text`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO profiles (user_id, display_name) VALUES ($1, 'T')`, id); err != nil {
		t.Fatal(err)
	}
	if talent {
		if _, err := tx.Exec(context.Background(), `INSERT INTO talent_profiles (user_id) VALUES ($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestOwnershipAndAttach(t *testing.T) {
	ctx := context.Background()
	tx := newTx(t)
	store, _ := media.NewLocalDisk(t.TempDir())
	svc := media.NewService(tx, store, "http://x", []byte("k"))
	alice, bob := newUser(t, tx, true), newUser(t, tx, true)

	photo, err := svc.Upload(ctx, alice, media.KindProfilePhoto, pngBytes(600, 400, false))
	if err != nil {
		t.Fatal(err)
	}
	if !photo.Public() || len(photo.Variants) != 2 {
		t.Fatalf("unexpected photo %+v", photo)
	}
	doc, err := svc.Upload(ctx, alice, media.KindVerificationDocument, []byte("%PDF-1.4\n%%EOF"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Public() || doc.Mime != media.MimePDF {
		t.Fatalf("pdf should be private: %+v", doc)
	}

	// Bob cannot attach or see-as-owned Alice's media.
	if err := svc.AttachTalentPhoto(ctx, bob, &photo.ID); !errors.Is(err, media.ErrNotFound) {
		t.Errorf("bob attach alice media: %v", err)
	}
	// Wrong kind: a document as a photo, a photo as a cover.
	if err := svc.AttachTalentPhoto(ctx, alice, &doc.ID); !errors.Is(err, media.ErrWrongKind) {
		t.Errorf("doc as photo: %v", err)
	}
	if err := svc.AttachTalentCover(ctx, alice, &photo.ID); !errors.Is(err, media.ErrWrongKind) {
		t.Errorf("photo as cover: %v", err)
	}
	if err := svc.AttachTalentPhoto(ctx, alice, &photo.ID); err != nil {
		t.Fatalf("own attach: %v", err)
	}
	// Private doc: owner and admin yes, other user no.
	m, _ := svc.Get(ctx, doc.ID)
	if m.CanView(media.Viewer{UserID: bob}) || !m.CanView(media.Viewer{UserID: alice}) || !m.CanView(media.Viewer{UserID: bob, IsAdmin: true}) {
		t.Error("private visibility rules")
	}
	// Only the owner can delete; deleting clears the profile reference.
	if err := svc.Delete(ctx, bob, photo.ID); !errors.Is(err, media.ErrNotFound) {
		t.Errorf("bob delete: %v", err)
	}
	if err := svc.Delete(ctx, alice, photo.ID); err != nil {
		t.Fatal(err)
	}
	var ref *string
	_ = tx.QueryRow(ctx, `SELECT photo_media_id::text FROM talent_profiles WHERE user_id = $1`, alice).Scan(&ref)
	if ref != nil {
		t.Error("photo reference not cleared")
	}
	if _, err := svc.Get(ctx, photo.ID); !errors.Is(err, media.ErrNotFound) {
		t.Errorf("deleted media still readable: %v", err)
	}
}

func pngBytes(w, h int, _ bool) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
