// Package media handles user uploads: validation, image processing, storage
// and authorization. Bytes live behind the Storage interface so the backend
// can move from local disk to Cloudflare R2 / S3 without touching callers.
package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var (
	ErrNotFoundInStorage = errors.New("media object not found")
	ErrInvalidKey        = errors.New("invalid storage key")
)

// Storage is the object-store port. Keys are always server-generated
// ("<uuid>/<variant>"); user-supplied filenames never reach it.
type Storage interface {
	// Put writes the object atomically, replacing any existing one.
	Put(ctx context.Context, key string, r io.Reader) (size int64, err error)
	// Open returns a reader for the object, or ErrNotFoundInStorage.
	Open(ctx context.Context, key string) (io.ReadSeekCloser, error)
	// Stat returns the object size, or ErrNotFoundInStorage.
	Stat(ctx context.Context, key string) (size int64, err error)
	// Delete removes the object; deleting a missing object is not an error.
	Delete(ctx context.Context, key string) error
}

// A future S3/R2 implementation (e.g. aws-sdk-go-v2 s3 client pointed at the R2
// endpoint) implements the same four methods: PutObject, GetObject (ranged for
// Seek), HeadObject, DeleteObject, with MEDIA_BUCKET / MEDIA_ENDPOINT env.
// Intentionally not implemented yet.

// keyPattern: "<uuid>/<variant>" with a lowercase variant name. Nothing else
// is ever accepted, so ".." and absolute paths cannot occur.
var keyPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/[a-z]{1,16}$`)

func validKey(key string) bool { return keyPattern.MatchString(key) }

// LocalDisk stores objects under Root.
type LocalDisk struct {
	Root string
}

func NewLocalDisk(root string) (*LocalDisk, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("create media dir: %w", err)
	}
	return &LocalDisk{Root: abs}, nil
}

func (d *LocalDisk) path(key string) (string, error) {
	if !validKey(key) {
		return "", ErrInvalidKey
	}
	return filepath.Join(d.Root, filepath.FromSlash(key)), nil
}

func (d *LocalDisk) Put(_ context.Context, key string, r io.Reader) (int64, error) {
	p, err := d.path(key)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".upload-*")
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(tmp, r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return 0, err
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		_ = os.Remove(tmp.Name())
		return 0, err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name())
		return 0, err
	}
	return n, nil
}

func (d *LocalDisk) Open(_ context.Context, key string) (io.ReadSeekCloser, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFoundInStorage
	}
	return f, err
}

func (d *LocalDisk) Stat(_ context.Context, key string) (int64, error) {
	p, err := d.path(key)
	if err != nil {
		return 0, err
	}
	st, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return 0, ErrNotFoundInStorage
	}
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func (d *LocalDisk) Delete(_ context.Context, key string) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.Remove(filepath.Dir(p)) // drop the empty per-media folder
	return nil
}
