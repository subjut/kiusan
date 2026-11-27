package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"

	godraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// The two stored sizes of an option image: the thumb sizes the answers
// table's column header (~64px cell, 2-3x on retina), the full sizes the
// click-to-open lightbox (sharp on 1080p/1440p desktops). Both are
// re-encoded JPEGs — the upload's original bytes never reach the store.
const (
	imageThumbMax = 256
	imageFullMax  = 1024
	imageJPEGQ    = 80
	// imageMaxSide rejects a source beyond 20000px on either side:
	// decoding one costs up to a couple of GiB of RGBA, which a
	// self-hosted box should not hand a single upload.
	imageMaxSide = 20000
)

// processUpload decodes an uploaded image, shrink-only resizes it into
// the two stored sizes (a source already at or below a target keeps its
// native size — it is never upscaled), and re-encodes both as JPEG. It
// returns the encoded thumb + full and the source dimensions; errCode
// names a rejection: "not_image" (nothing the decoders can read) or
// "too_big" (beyond imageMaxSide).
func processUpload(data []byte) (thumb, full []byte, w, h int, errCode string) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, nil, 0, 0, "not_image"
	}
	if cfg.Width > imageMaxSide || cfg.Height > imageMaxSide {
		return nil, nil, 0, 0, "too_big"
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, 0, 0, "not_image"
	}
	size := func(max int) []byte {
		b := src.Bounds()
		sw, sh := b.Dx(), b.Dy()
		if sw <= max && sh <= max {
			return encodeJPEG(flattenAlpha(src))
		}
		scale := float64(max) / math.Max(float64(sw), float64(sh))
		nw := int(math.Round(float64(sw) * scale))
		nh := int(math.Round(float64(sh) * scale))
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		godraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
		return encodeJPEG(dst)
	}
	return size(imageThumbMax), size(imageFullMax), cfg.Width, cfg.Height, ""
}

// flattenAlpha composites src over a white base: a transparent source
// (PNG, GIF) must not render as black in the opaque JPEG output.
func flattenAlpha(src image.Image) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	draw.Draw(dst, b, image.NewUniform(color.White), image.Point{}, draw.Over)
	draw.Draw(dst, b, src, b.Min, draw.Over)
	return dst
}

// encodeJPEG renders img at quality imageJPEGQ. jpeg.Encode cannot fail
// for the formats this pipeline produces (NRGBA/RGBA), so the error is
// dropped.
func encodeJPEG(img image.Image) []byte {
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: imageJPEGQ})
	return buf.Bytes()
}
