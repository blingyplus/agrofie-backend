package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strconv"
	"strings"
	"testing"
	"time"
)

func pngBytes(w, h int, alpha bool) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := uint8(255)
			if alpha && x < w/2 {
				a = 0
			}
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 90, A: a})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func TestSniffMime(t *testing.T) {
	jpg := new(bytes.Buffer)
	_ = jpeg.Encode(jpg, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil)
	heic := append([]byte{0, 0, 0, 24}, []byte("ftypheic")...)
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"png", pngBytes(4, 4, false), MimePNG},
		{"jpeg", jpg.Bytes(), MimeJPEG},
		{"pdf", []byte("%PDF-1.4\n..."), MimePDF},
		{"heic", append(heic, make([]byte, 20)...), MimeHEIC},
		{"webp", append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), make([]byte, 20)...), MimeWebP},
		{"html disguised", []byte("<html><script>alert(1)</script>"), ""},
		{"exe", []byte("MZ\x90\x00\x03"), ""},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), ""},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		if got := SniffMime(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestCheckUploadPolicy(t *testing.T) {
	cases := []struct {
		kind, mime string
		size       int64
		want       error
	}{
		{KindProfilePhoto, MimeJPEG, 1000, nil},
		{KindProfilePhoto, MimePDF, 1000, ErrInvalidInput},
		{KindCoverImage, MimePNG, MaxImageBytes + 1, ErrTooLarge},
		{KindCoverImage, MimePNG, MaxImageBytes, nil},
		{KindVerificationDocument, MimePDF, MaxPDFBytes, nil},
		{KindVerificationDocument, MimePDF, MaxPDFBytes + 1, ErrTooLarge},
		{KindVerificationDocument, MimeJPEG, MaxImageBytes + 1, ErrTooLarge},
		{KindVerificationDocument, MimeHEIC, 10, ErrHEIC},
		{KindProfilePhoto, "", 10, ErrUnsupportedType},
		{"nope", MimeJPEG, 10, ErrUnknownKind},
	}
	for _, c := range cases {
		if err := CheckUpload(c.kind, c.mime, c.size); !errors.Is(err, c.want) {
			t.Errorf("%s %s %d: got %v want %v", c.kind, c.mime, c.size, err, c.want)
		}
	}
}

func TestProcessImageVariantsAndStrip(t *testing.T) {
	vs, err := ProcessImage(pngBytes(3000, 1500, true))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{"original": {2048, 1024}, "medium": {1024, 512}, "thumb": {256, 128}}
	for _, v := range vs {
		w := want[v.Name]
		if v.Width != w[0] || v.Height != w[1] {
			t.Errorf("%s: %dx%d want %v", v.Name, v.Width, v.Height, w)
		}
		if SniffMime(v.Data) != MimeJPEG {
			t.Errorf("%s not jpeg", v.Name)
		}
	}
	// small images are not upscaled
	vs, _ = ProcessImage(pngBytes(100, 50, false))
	if vs[0].Width != 100 {
		t.Errorf("upscaled: %d", vs[0].Width)
	}
}

func TestProcessImageRejectsGarbageAndBombs(t *testing.T) {
	if _, err := ProcessImage([]byte("\x89PNG\r\n\x1a\nnot really")); !errors.Is(err, ErrBadImage) {
		t.Errorf("garbage: %v", err)
	}
	// A valid PNG header claiming 20000x20000 (400MP) must be rejected before decoding.
	b := pngBytes(2, 2, false)
	binary.BigEndian.PutUint32(b[16:20], 20000)
	binary.BigEndian.PutUint32(b[20:24], 20000)
	if _, err := ProcessImage(b); err == nil {
		t.Error("expected rejection of oversized dimensions")
	}
}

// exifJPEG builds a tiny JPEG whose EXIF orientation is o.
func exifJPEG(t *testing.T, o uint16) []byte {
	var j bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 40, 20)) // landscape
	_ = jpeg.Encode(&j, img, nil)
	raw := j.Bytes()
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01")
	tiff = append(tiff, byte(o>>8), byte(o), 0, 0, 0, 0, 0, 0)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}
	app1 = append(app1, seg...)
	return append(append(append([]byte{}, raw[:2]...), app1...), raw[2:]...)
}

func TestEXIFOrientationApplied(t *testing.T) {
	if got := jpegOrientation(exifJPEG(t, 6)); got != 6 {
		t.Fatalf("orientation parse: %d", got)
	}
	vs, err := ProcessImage(exifJPEG(t, 6)) // rotate 90: 40x20 -> 20x40
	if err != nil {
		t.Fatal(err)
	}
	if vs[0].Width != 20 || vs[0].Height != 40 {
		t.Errorf("not auto-oriented: %dx%d", vs[0].Width, vs[0].Height)
	}
	// output carries no EXIF
	if bytes.Contains(vs[0].Data, []byte("Exif")) {
		t.Error("EXIF not stripped")
	}
	// malformed EXIF never panics
	for n := 0; n < 40; n++ {
		_ = jpegOrientation(exifJPEG(t, 6)[:n])
	}
}

func TestCheckPDF(t *testing.T) {
	ok := []byte("%PDF-1.4\n1 0 obj<<>>endobj\n%%EOF")
	if err := checkPDF(ok); err != nil {
		t.Errorf("valid pdf rejected: %v", err)
	}
	for name, b := range map[string][]byte{
		"truncated": []byte("%PDF-1.4\n1 0 obj"),
		"js":        []byte("%PDF-1.4\n<< /JavaScript (x) >>\n%%EOF"),
		"launch":    []byte("%PDF-1.4\n<< /Launch >>\n%%EOF"),
	} {
		if err := checkPDF(b); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestLocalDiskKeysAndPathSafety(t *testing.T) {
	d, err := NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	good := "0b6c2f1e-1111-4222-8333-444455556666/original"
	if _, err := d.Put(ctx, good, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if n, err := d.Stat(ctx, good); err != nil || n != 5 {
		t.Fatalf("stat %d %v", n, err)
	}
	for _, bad := range []string{"../etc/passwd", "/etc/passwd", "a/../../b", "0b6c2f1e-1111-4222-8333-444455556666/../x", "..", "", "x/original", good + "/../.."} {
		if _, err := d.Put(ctx, bad, strings.NewReader("x")); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Put(%q) = %v, want ErrInvalidKey", bad, err)
		}
		if _, err := d.Open(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Open(%q) = %v", bad, err)
		}
	}
	if err := d.Delete(ctx, good); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Stat(ctx, good); !errors.Is(err, ErrNotFoundInStorage) {
		t.Errorf("after delete: %v", err)
	}
	if err := d.Delete(ctx, good); err != nil {
		t.Errorf("double delete: %v", err)
	}
}

func TestAuthorization(t *testing.T) {
	priv := &Media{OwnerID: "u1", Visibility: "private", Status: "active"}
	pub := &Media{OwnerID: "u1", Visibility: "public", Status: "active"}
	gone := &Media{OwnerID: "u1", Visibility: "public", Status: "deleted"}
	tests := []struct {
		name string
		m    *Media
		v    Viewer
		want bool
	}{
		{"public anon", pub, Viewer{}, true},
		{"private anon", priv, Viewer{}, false},
		{"private owner", priv, Viewer{UserID: "u1"}, true},
		{"private other", priv, Viewer{UserID: "u2"}, false},
		{"private admin", priv, Viewer{UserID: "a", IsAdmin: true}, true},
		{"deleted", gone, Viewer{UserID: "u1", IsAdmin: true}, false},
	}
	for _, c := range tests {
		if got := c.m.CanView(c.v); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestSignedURL(t *testing.T) {
	s := &Service{baseURL: "http://x", secret: []byte("k")}
	id := "0b6c2f1e-1111-4222-8333-444455556666"
	exp := time.Now().Add(time.Minute).Unix()
	sig := s.sign(id, "", exp)
	if !s.VerifySignature(id, "", itoa(exp), sig) {
		t.Error("valid signature rejected")
	}
	if s.VerifySignature(id, "thumb", itoa(exp), sig) {
		t.Error("signature reused for another variant")
	}
	if s.VerifySignature("other", "", itoa(exp), sig) {
		t.Error("signature reused for another id")
	}
	old := time.Now().Add(-time.Minute).Unix()
	if s.VerifySignature(id, "", itoa(old), s.sign(id, "", old)) {
		t.Error("expired signature accepted")
	}
	if !strings.Contains(s.SignedURL(id, "medium", time.Minute), "?v=medium&exp=") {
		t.Error("signed url shape")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestLimiter(t *testing.T) {
	l := newLimiter(2, time.Hour)
	if !l.allow("a") || !l.allow("a") || l.allow("a") {
		t.Error("limit not enforced")
	}
	if !l.allow("b") {
		t.Error("keys must be independent")
	}
}
