package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // register decoders
	"net/http"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // register decoder (BSD-3)
)

const (
	MimeJPEG = "image/jpeg"
	MimePNG  = "image/png"
	MimeWebP = "image/webp"
	MimeHEIC = "image/heic"
	MimePDF  = "application/pdf"

	// maxPixels guards against decompression bombs (a tiny PNG that expands to GBs).
	maxPixels = 40_000_000
	jpegQ     = 85
)

var (
	ErrUnsupportedType = errors.New("unsupported file type")
	ErrHEIC            = errors.New("HEIC/HEIF images are not supported: convert to JPEG first")
	ErrTooManyPixels   = errors.New("image dimensions too large")
	ErrBadImage        = errors.New("could not read image")
)

// SniffMime decides the real content type from the bytes, ignoring the
// client-declared type and filename.
func SniffMime(b []byte) string {
	if len(b) >= 12 && string(b[4:8]) == "ftyp" {
		switch string(b[8:12]) {
		case "heic", "heix", "hevc", "hevx", "mif1", "msf1", "heim", "heis":
			return MimeHEIC
		}
	}
	if bytes.HasPrefix(b, []byte("%PDF-")) {
		return MimePDF
	}
	switch m := http.DetectContentType(b); m {
	case MimeJPEG, MimePNG, MimeWebP:
		return m
	}
	return ""
}

// Variant is one processed rendition of an image.
type Variant struct {
	Name          string
	Data          []byte
	Width, Height int
}

type variantSpec struct {
	name string
	max  int
}

// Variants stored for every image. "original" is the sanitized upload capped at 2048px.
var variantSpecs = []variantSpec{{"original", 2048}, {"medium", 1024}, {"thumb", 256}}

// ProcessImage decodes (any of jpeg/png/webp), applies EXIF orientation,
// flattens transparency onto white, strips all metadata by re-encoding, and
// returns the JPEG renditions. Output is always JPEG.
func ProcessImage(data []byte) ([]Variant, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, ErrTooManyPixels
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadImage, err)
	}
	src = orient(src, jpegOrientation(data))
	src = flatten(src)

	out := make([]Variant, 0, len(variantSpecs))
	for _, spec := range variantSpecs {
		img := fit(src, spec.max)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQ}); err != nil {
			return nil, fmt.Errorf("encode %s: %w", spec.name, err)
		}
		b := img.Bounds()
		out = append(out, Variant{Name: spec.name, Data: buf.Bytes(), Width: b.Dx(), Height: b.Dy()})
	}
	return out, nil
}

// fit downscales so the long edge is at most max; never upscales.
func fit(src image.Image, max int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return src
	}
	var nw, nh int
	if w >= h {
		nw, nh = max, h*max/w
	} else {
		nh, nw = max, w*max/h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

func flatten(src image.Image) image.Image {
	if op, ok := src.(interface{ Opaque() bool }); ok && op.Opaque() {
		return src
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

// orient applies an EXIF orientation value (1-8) so pixels are upright.
func orient(src image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	swap := o >= 5
	dw, dh := w, h
	if swap {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var nx, ny int
			switch o {
			case 2: // mirror horizontal
				nx, ny = w-1-x, y
			case 3: // rotate 180
				nx, ny = w-1-x, h-1-y
			case 4: // mirror vertical
				nx, ny = x, h-1-y
			case 5: // transpose
				nx, ny = y, x
			case 6: // rotate 90 CW
				nx, ny = h-1-y, x
			case 7: // transverse
				nx, ny = h-1-y, w-1-x
			case 8: // rotate 90 CCW
				nx, ny = y, w-1-x
			}
			dst.Set(nx, ny, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// jpegOrientation extracts the EXIF Orientation tag from a JPEG; 1 if absent
// or not a JPEG. Bounds-checked: malformed data yields 1, never a panic.
func jpegOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xDA || marker == 0xD9 { // start of scan / EOI
			return 1
		}
		segLen := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if segLen < 2 || i+2+segLen > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+segLen]
		if marker == 0xE1 && len(seg) >= 14 && string(seg[:6]) == "Exif\x00\x00" {
			return exifOrientation(seg[6:])
		}
		i += 2 + segLen
	}
	return 1
}

func exifOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off : off+2]))
	for k := 0; k < n; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:e+2]) == 0x0112 {
			v := int(bo.Uint16(t[e+8 : e+10]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
