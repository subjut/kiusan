package main

import (
	"errors"
	"io"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// The upload limits: a phone photo (10-16 MP) is the largest sensible
// source — everything above is discarded by the resize anyway — and the
// file count is capped at the option count.
const (
	maxUploadBytes = 10 << 20 // 10 MB per file
	maxUploadFiles = maxChoices
)

// imageResult is one uploaded file's outcome: on success the stored
// image's ID + source dimensions, on failure the rejection's code
// ("too_large" | "too_big" | "not_image" | "internal") — the form page
// maps the codes onto localized messages.
type imageResult struct {
	ID  string `json:"id,omitempty"`
	W   int    `json:"w"`
	H   int    `json:"h"`
	Err string `json:"err,omitempty"`
}

// voteImageUploadHandler accepts the form page's image uploads (multipart
// field "files", up to maxUploadFiles files of maxUploadBytes each):
// every file is decoded, shrink-resized into both stored sizes, re-encoded
// as JPEG, and stored under a fresh random ID — which the page embeds in
// the option row's hidden field on the next (url-encoded) form submit.
func voteImageUploadHandler(c fiber.Ctx) error {
	form, err := c.MultipartForm()
	if err != nil {
		return sendErr(c, 400, "Invalid upload")
	}
	files := form.File["files"]
	if len(files) == 0 {
		return sendErr(c, 400, "No files")
	}
	if len(files) > maxUploadFiles {
		return sendErr(c, 400, "Too many files (max 20)")
	}

	now := time.Now()
	results := make([]imageResult, 0, len(files))
	for _, fh := range files {
		res := imageResult{}
		switch {
		case fh.Size > maxUploadBytes:
			res.Err = "too_large"
		default:
			f, err := fh.Open()
			if err != nil {
				res.Err = "internal"
			} else {
				data, err := io.ReadAll(f)
				_ = f.Close()
				if err != nil {
					res.Err = "internal"
				} else {
					thumb, full, w, h, code := processUpload(data)
					if code != "" {
						res.Err = code
					} else {
						id, err := store.RandomKey(12)
						if err != nil {
							res.Err = "internal"
						} else if err := db.PutVoteImage(id, thumb, full, now); err != nil {
							res.Err = "internal"
						} else {
							res.ID, res.W, res.H = id, w, h
						}
					}
				}
			}
		}
		results = append(results, res)
	}
	return c.JSON(results)
}

// sendVoteImage serves one stored size of an option image: immutable
// (the ID is a random, content-stable token), jpeg content type.
func sendVoteImage(c fiber.Ctx, kind string) error {
	data, err := db.GetVoteImage(c.Params("+"), kind)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return notFoundHandler(c)
		}
		return sendInternal(c, err)
	}
	c.Set("Cache-Control", "private, max-age=31536000, immutable")
	c.Set("Content-Type", "image/jpeg")
	return c.Send(data)
}

// serveVoteImageFull / serveVoteImageThumb are the two routes' handlers.
func serveVoteImageFull(c fiber.Ctx) error {
	return sendVoteImage(c, database.VoteImageKindFull)
}

func serveVoteImageThumb(c fiber.Ctx) error {
	return sendVoteImage(c, database.VoteImageKindThumb)
}
