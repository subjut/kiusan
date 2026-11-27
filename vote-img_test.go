package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
)

// uploadPart is one file of a multipart upload body.
type uploadPart struct {
	filename string
	data     []byte
}

// multipartBody builds the upload body: the "files" field, one part per
// entry, in order.
func multipartBody(boundary string, parts []uploadPart) []byte {
	var buf bytes.Buffer
	for _, p := range parts {
		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		fmt.Fprintf(&buf, "Content-Disposition: form-data; name=\"files\"; filename=\"%s\"\r\n", p.filename)
		buf.WriteString("Content-Type: application/octet-stream\r\n\r\n")
		buf.Write(p.data)
		buf.WriteString("\r\n")
	}
	fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	return buf.Bytes()
}

// postUpload POSTs a multipart body to /vote-img, like the form page's
// fetch does.
func postUpload(t *testing.T, app *fiber.App, parts []uploadPart) *http.Response {
	t.Helper()
	const boundary = "test-boundary"
	req := httptest.NewRequest(http.MethodPost, "/vote-img", bytes.NewReader(multipartBody(boundary, parts)))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	r, err := app.Test(req)
	if err != nil {
		t.Fatalf("POST /vote-img: %v", err)
	}
	return r
}

// uploadResults parses the upload's JSON result list.
func uploadResults(t *testing.T, r *http.Response) []imageResult {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reading upload response: %v", err)
	}
	var res []imageResult
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatalf("parsing upload results: %v (body: %s)", err, b)
	}
	return res
}

// TestVoteImageUpload pins the upload contract: one result per file in
// order (the stored ID + source dimensions on success, the rejection code
// on failure), both sizes stored under the ID, the oversize + per-file-
// type gates, the file count cap, and a files-less request 400.
func TestVoteImageUpload(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)

	t.Run("one good file stores both sizes", func(t *testing.T) {
		r := postUpload(t, app, []uploadPart{{"a.png", testPNG(12, 8)}})
		if r.StatusCode != http.StatusOK {
			t.Fatalf("status %d, want 200", r.StatusCode)
		}
		res := uploadResults(t, r)
		if len(res) != 1 || res[0].Err != "" || res[0].ID == "" || res[0].W != 12 || res[0].H != 8 {
			t.Fatalf("unexpected results: %+v", res)
		}
		for _, kind := range []string{database.VoteImageKindThumb, database.VoteImageKindFull} {
			if _, err := db.GetVoteImage(res[0].ID, kind); err != nil {
				t.Fatalf("stored %s of %q: %v", kind, res[0].ID, err)
			}
		}
	})

	t.Run("a batch keeps the per-file results in order", func(t *testing.T) {
		r := postUpload(t, app, []uploadPart{
			{"a.png", testPNG(4, 4)},
			{"x.bin", []byte("not an image")},
		})
		if r.StatusCode != http.StatusOK {
			t.Fatalf("status %d, want 200", r.StatusCode)
		}
		res := uploadResults(t, r)
		if len(res) != 2 {
			t.Fatalf("want 2 results, got %d: %+v", len(res), res)
		}
		if res[0].Err != "" || res[0].ID == "" {
			t.Fatalf("first file must succeed: %+v", res[0])
		}
		if res[1].Err != "not_image" {
			t.Fatalf("second file must be rejected not_image: %+v", res[1])
		}
	})

	t.Run("an oversize file is rejected before decode", func(t *testing.T) {
		// The size gate runs before the decoders, so the oversized part
		// needs no image bytes at all.
		r := postUpload(t, app, []uploadPart{{"big.bin", bytes.Repeat([]byte{0}, maxUploadBytes+1)}})
		if r.StatusCode != http.StatusOK {
			t.Fatalf("status %d, want 200", r.StatusCode)
		}
		res := uploadResults(t, r)
		if len(res) != 1 || res[0].Err != "too_large" {
			t.Fatalf("want one too_large result, got: %+v", res)
		}
	})

	t.Run("no files is a 400", func(t *testing.T) {
		r := postUpload(t, app, nil)
		if r.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d, want 400", r.StatusCode)
		}
	})

	t.Run("the file count cap", func(t *testing.T) {
		parts := make([]uploadPart, maxUploadFiles+1)
		for i := range parts {
			parts[i] = uploadPart{fmt.Sprintf("%d.png", i), testPNG(4, 4)}
		}
		r := postUpload(t, app, parts)
		if r.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d, want 400 for %d files", r.StatusCode, len(parts))
		}
	})
}

// TestServeVoteImage pins the two serve routes: the full and the thumb
// bytes, the immutable jpeg cache headers, and a 404 for an unknown ID.
func TestServeVoteImage(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)

	const id = "abc123def456"
	thumb, full := testPNG(2, 2), testPNG(3, 3)
	if err := db.PutVoteImage(id, thumb, full, time.Now()); err != nil {
		t.Fatalf("PutVoteImage: %v", err)
	}

	get := func(path string) (*http.Response, []byte) {
		t.Helper()
		r, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		return r, b
	}

	t.Run("the full size", func(t *testing.T) {
		r, b := get("/vote-img/" + id)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("status %d, want 200", r.StatusCode)
		}
		if ct := r.Header.Get("Content-Type"); ct != "image/jpeg" {
			t.Fatalf("content type %q, want image/jpeg", ct)
		}
		if cc := r.Header.Get("Cache-Control"); cc != "private, max-age=31536000, immutable" {
			t.Fatalf("cache control %q, want the immutable policy", cc)
		}
		if !bytes.Equal(b, full) {
			t.Fatalf("served %d bytes, want the stored %d full bytes", len(b), len(full))
		}
	})

	t.Run("the thumb size", func(t *testing.T) {
		r, b := get("/vote-img/" + id + "/thumb")
		if r.StatusCode != http.StatusOK {
			t.Fatalf("status %d, want 200", r.StatusCode)
		}
		if !bytes.Equal(b, thumb) {
			t.Fatalf("served %d bytes, want the stored %d thumb bytes", len(b), len(thumb))
		}
	})

	t.Run("an unknown ID is a 404", func(t *testing.T) {
		for _, path := range []string{"/vote-img/does-not-exist", "/vote-img/does-not-exist/thumb"} {
			r, _ := get(path)
			if r.StatusCode != http.StatusNotFound {
				t.Fatalf("GET %s: status %d, want 404", path, r.StatusCode)
			}
		}
	})
}
