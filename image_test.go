package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// pngHeader fabricates a barest PNG: the signature plus an IHDR carrying
// w x h, nothing after. It decodes its *config* fine (processUpload's
// "too_big" gate only needs that) and fails the full decode (the
// "not_image" path) — no multi-megabyte fixture needed for either.
func pngHeader(w, h int) []byte {
	b := []byte{'\x89', 'P', 'N', 'G', '\r', '\n', '\x1a', '\n'}
	var ihdr [13]byte
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(h))
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // color type: RGBA
	payload := append([]byte("IHDR"), ihdr[:]...)
	b = append(b, 0, 0, 0, 13)
	b = append(b, payload...)
	csum := crc32.ChecksumIEEE(payload)
	b = append(b, byte(csum>>24), byte(csum>>16), byte(csum>>8), byte(csum))
	return b
}

// testPNG encodes a w x h RGBA image with a per-pixel gradient (so the
// bytes are non-trivial) as PNG.
func testPNG(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 7), uint8(y * 11), 128, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func jpegSize(t *testing.T, data []byte) (int, int) {
	t.Helper()
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("not a JPEG (first bytes % x)", data[:min(4, len(data))])
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding JPEG: %v", err)
	}
	return cfg.Width, cfg.Height
}

// TestProcessUpload pins the upload pipeline: shrink-only resize into the
// two stored sizes (a source already at or below a target is never
// upscaled), the JPEG re-encode, and the two rejection codes.
func TestProcessUpload(t *testing.T) {
	t.Run("large source shrinks to both stored sizes", func(t *testing.T) {
		thumb, full, w, h, code := processUpload(testPNG(300, 200))
		if code != "" {
			t.Fatalf("processUpload: rejection %q, want none", code)
		}
		if w != 300 || h != 200 {
			t.Fatalf("source dimensions %dx%d, want 300x200", w, h)
		}
		if tw, th := jpegSize(t, thumb); tw != 256 || th != 171 {
			t.Fatalf("thumb %dx%d, want 256x171 (300x200 scaled to the 256px cap)", tw, th)
		}
		if fw, fh := jpegSize(t, full); fw != 300 || fh != 200 {
			t.Fatalf("full %dx%d, want 300x200 (below the 1024px cap: kept native)", fw, fh)
		}
	})

	t.Run("small source keeps its native size", func(t *testing.T) {
		thumb, full, w, h, code := processUpload(testPNG(100, 50))
		if code != "" {
			t.Fatalf("processUpload: rejection %q, want none", code)
		}
		if w != 100 || h != 50 {
			t.Fatalf("source dimensions %dx%d, want 100x50", w, h)
		}
		if tw, th := jpegSize(t, thumb); tw != 100 || th != 50 {
			t.Fatalf("thumb %dx%d, want 100x50 (never upscaled)", tw, th)
		}
		if fw, fh := jpegSize(t, full); fw != 100 || fh != 50 {
			t.Fatalf("full %dx%d, want 100x50 (never upscaled)", fw, fh)
		}
	})

	t.Run("gif decodes too", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 16, 9))
		img.SetNRGBA(3, 2, color.NRGBA{200, 40, 40, 255})
		var buf bytes.Buffer
		if err := gif.Encode(&buf, img, nil); err != nil {
			t.Fatalf("encoding GIF: %v", err)
		}
		thumb, full, w, h, code := processUpload(buf.Bytes())
		if code != "" {
			t.Fatalf("processUpload: rejection %q, want none", code)
		}
		if w != 16 || h != 9 {
			t.Fatalf("source dimensions %dx%d, want 16x9", w, h)
		}
		jpegSize(t, thumb)
		jpegSize(t, full)
	})

	t.Run("not an image", func(t *testing.T) {
		if _, _, _, _, code := processUpload([]byte("definitely not an image")); code != "not_image" {
			t.Fatalf("rejection %q, want not_image", code)
		}
	})

	t.Run("a header with a corrupt body", func(t *testing.T) {
		// A valid IHDR, no image data: the config decodes, the body
		// doesn't — the decode failure maps onto not_image.
		if _, _, _, _, code := processUpload(pngHeader(100, 50)); code != "not_image" {
			t.Fatalf("rejection %q, want not_image", code)
		}
	})

	t.Run("beyond the source-side cap", func(t *testing.T) {
		// The gate runs on the header's dimensions, before any decode —
		// a 20001px source must be rejected without the GiB of RGBA it
		// would cost.
		if _, _, _, _, code := processUpload(pngHeader(20001, 10)); code != "too_big" {
			t.Fatalf("rejection %q, want too_big", code)
		}
	})
}

// TestFlattenAlpha pins the transparent-over-white composite: the stored
// JPEG must render transparency as white, never as the black that a raw
// encode of an alpha image would give.
func TestFlattenAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.SetNRGBA(0, 0, color.NRGBA{0, 0, 0, 0})     // fully transparent
	src.SetNRGBA(1, 0, color.NRGBA{0, 0, 0, 128})   // half black
	src.SetNRGBA(0, 1, color.NRGBA{255, 0, 0, 255}) // opaque red
	src.SetNRGBA(1, 1, color.NRGBA{0, 0, 0, 255})   // opaque black

	dst := flattenAlpha(src)
	check := func(x, y int, wantR, wantG, wantB int, tol int) {
		t.Helper()
		c := dst.NRGBAAt(x, y)
		if c.A != 255 {
			t.Fatalf("pixel (%d,%d) not opaque: alpha %d", x, y, c.A)
		}
		if int(c.R) < wantR-tol || int(c.R) > wantR+tol ||
			int(c.G) < wantG-tol || int(c.G) > wantG+tol ||
			int(c.B) < wantB-tol || int(c.B) > wantB+tol {
			t.Fatalf("pixel (%d,%d) = (%d,%d,%d), want (~%d,%d,%d)", x, y, c.R, c.G, c.B, wantR, wantG, wantB)
		}
	}
	check(0, 0, 255, 255, 255, 0) // transparency -> white
	check(1, 0, 127, 127, 127, 2) // half black over white -> mid gray
	check(0, 1, 255, 0, 0, 0)     // opaque red
	check(1, 1, 0, 0, 0, 0)       // opaque black
}

// TestEncodeJPEG pins the re-encode: a valid JPEG of the exact size, the
// bytes the store persists.
func TestEncodeJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.SetRGBA(1, 1, color.RGBA{10, 20, 30, 255})
	b := encodeJPEG(img)
	w, h := jpegSize(t, b)
	if w != 4 || h != 3 {
		t.Fatalf("encoded JPEG %dx%d, want 4x3", w, h)
	}
	if _, err := jpeg.Decode(bytes.NewReader(b)); err != nil {
		t.Fatalf("the encoded JPEG must decode: %v", err)
	}
}
