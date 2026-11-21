package main

import (
	_ "embed"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	// Bundle Go's zoneinfo so time.LoadLocation works on hosts without
	// system tzdata (Alpine, minimal containers).
	_ "time/tzdata"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/gofiber/template/html/v3"

	"github.com/subjut/kiusan/store"
)

// db is the storage backend for the whole app. Handlers talk to storage
// only through it; tests swap it for a throwaway instance per run.
var db store.Store

// Runtime configuration (cfg, filled by loadConfig in config.go): every
// key resolves env (KIUSAN_*) > the data dir's kiusan.yaml > the
// built-in default.

// Per-client rate limits on the unauthenticated write routes.
const (
	rateWindow         = time.Minute
	createMaxPerMinute = 20
	answerMaxPerMinute = 30
)

//go:embed data/timezones.txt
var timezoneNamesRaw string

// timezoneList is the IANA zones offered in the form's timezone select:
// every zone from the committed data/timezones.txt (curated list, grouped
// by UTC offset) that this build can actually load, so an offered zone can
// never fail to load in the meeting view. Sorted alphabetically for the
// dropdown, independent of the file's grouping. Embedded rather than read
// from disk so it works regardless of the working directory (tests chdir).
var timezoneList = func() []string {
	zones := loadableTimezones(strings.Split(timezoneNamesRaw, "\n"))
	sort.Strings(zones)
	return zones
}()

// zoneOptionLabels is the dropdown option label per zone: the display name
// plus the zone's standard (non-DST) UTC offset in parentheses, e.g.
// "Europe/Berlin (UTC+1)". Precomputed at process start; a zone the server
// can load but the list doesn't offer (the browser's own zone, inserted by
// dater.js) falls back to computing the label on demand.
var zoneOptionLabels = func() map[string]string {
	labels := make(map[string]string, len(timezoneList))
	for _, z := range timezoneList {
		labels[z] = zoneOptionLabel(z)
	}
	return labels
}()

// zoneLabel renders a zone for display: Etc/GMT±N zones become compact
// UTC±N labels (the IANA sign is inverted: Etc/GMT-5 is UTC+5), every other
// name gets its underscores as spaces. Option values keep the real IANA
// name, so the form still submits a loadable zone.
func zoneLabel(z string) string {
	switch {
	case z == "Etc/GMT":
		return "UTC"
	case strings.HasPrefix(z, "Etc/GMT-"):
		return "UTC+" + z[len("Etc/GMT-"):]
	case strings.HasPrefix(z, "Etc/GMT+"):
		return "UTC-" + z[len("Etc/GMT+"):]
	default:
		return strings.ReplaceAll(z, "_", " ")
	}
}

// standardOffset is a zone's standard (non-DST) UTC offset in seconds. DST
// always advances the clock, so the smaller of the January/July offsets is
// the standard one — true in both hemispheres.
func standardOffset(loc *time.Location) int {
	jan := time.Date(2020, 1, 1, 12, 0, 0, 0, loc)
	jul := time.Date(2020, 7, 1, 12, 0, 0, 0, loc)
	_, janOff := jan.Zone()
	_, julOff := jul.Zone()
	if janOff < julOff {
		return janOff
	}
	return julOff
}

// offsetLabel renders a UTC offset in seconds compactly: 0 is plain UTC,
// whole hours UTC±H, fractional offsets add :MM (UTC+5:30, UTC+5:45).
func offsetLabel(offsetSec int) string {
	if offsetSec == 0 {
		return "UTC"
	}
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	h := offsetSec / 3600
	m := (offsetSec % 3600) / 60
	if m == 0 {
		return "UTC" + sign + strconv.Itoa(h)
	}
	mm := strconv.Itoa(m)
	if len(mm) < 2 {
		mm = "0" + mm
	}
	return "UTC" + sign + strconv.Itoa(h) + ":" + mm
}

// zoneOptionLabel is the dropdown's option label: the display name
// (zoneLabel) plus the zone's standard (non-DST) UTC offset in
// parentheses — "Europe/Berlin (UTC+1)". Bare UTC stands alone; an
// unloadable zone renders its name without the offset.
func zoneOptionLabel(z string) string {
	if z == "UTC" {
		return zoneLabel(z)
	}
	loc, err := time.LoadLocation(z)
	if err != nil {
		return zoneLabel(z)
	}
	return zoneLabel(z) + " (" + offsetLabel(standardOffset(loc)) + ")"
}

// loadableTimezones drops names the server cannot load; the list is
// validated once at process start, not per request. Blank lines and `#`
// group-comment lines in the data file are skipped.
func loadableTimezones(names []string) []string {
	zones := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || strings.HasPrefix(n, "#") {
			continue
		}
		if _, err := time.LoadLocation(n); err != nil {
			log.Printf("warning: dropping unloadable timezone %q: %v", n, err)
			continue
		}
		zones = append(zones, n)
	}
	return zones
}

func initRoutes(app *fiber.App, staticDir string) {
	// Security headers on every response: refuse to render inside any frame
	// (clickjacking), a strict same-origin CSP (the site uses no inline
	// scripts or styles), no MIME sniffing, and a minimal referrer policy.
	app.Use(func(c fiber.Ctx) error {
		c.Set("X-Frame-Options", "DENY")
		c.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; "+
				"img-src 'self' data:; object-src 'none'; base-uri 'self'; "+
				"frame-ancestors 'none'")
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Referrer-Policy", "same-origin")
		return c.Next()
	})

	// The i18n endpoints: the page's initial dictionary as a same-origin
	// script (the CSP allows no inline scripts) and the per-language
	// dictionaries the dropdown fetches on a switch.
	app.Get("/i18n/boot", i18nBootHandler)
	app.Get("/i18n/:lang", i18nDictHandler)

	// Make sure style files, fonts, and favicons are available
	// (served from the site root; a file miss resets the response and calls Next)
	app.Get("/*", static.New(filepath.Join(staticDir, "css")))
	app.Get("/*", static.New(filepath.Join(staticDir, "js")))
	app.Get("/*", static.New(filepath.Join(staticDir, "fonts")))
	app.Get("/*", static.New(filepath.Join(staticDir, "favicon")))

	// Rate limits on the unauthenticated write routes: poll and vote
	// creation and answering are open, so bound the per-client request
	// rate. Per-poll cost is already capped (slot count, field lengths);
	// this caps the rate.
	createLimit := limiter.New(limiter.Config{Max: createMaxPerMinute, Expiration: rateWindow})
	answerLimit := limiter.New(limiter.Config{Max: answerMaxPerMinute, Expiration: rateWindow})

	// Normal routes
	app.Get("/", homeHandler)

	// Meeting form routes.
	app.Get("/meet-form", meetFormHandler)
	app.Post("/meet-form", createLimit, meetFormPostHandler)
	// The edit form is reachable by the admin link only.
	app.Get("/meet-form/edit/+", meetFormEditHandler)

	// Vote form routes.
	app.Get("/vote-form", voteFormHandler)
	app.Post("/vote-form", createLimit, voteFormPostHandler)
	app.Get("/vote-form/edit/+", voteFormEditHandler)

	// Vote page routes; the gobbler takes either link key of the vote.
	app.Get("/vote/+", voteHandler)
	app.Post("/vote/+/answer", answerLimit, voteOps.postAnswer())
	app.Post("/vote/+/answer/edit", answerLimit, voteOps.editAnswer())
	app.Post("/vote/+/answer/delete", answerLimit, voteOps.deleteAnswer())

	// Vote lifecycle, admin link only.
	app.Post("/vote/+/close", voteOps.closePoll())
	app.Post("/vote/+/reopen", voteOps.reopenPoll())
	app.Post("/vote/+/delete", voteOps.deletePoll())

	// Meeting page routes; the gobbler takes either link key of the poll.
	app.Get("/meet/+", meetingHandler)
	app.Post("/meet/+/answer", answerLimit, meetOps.postAnswer())
	app.Post("/meet/+/answer/edit", answerLimit, meetOps.editAnswer())
	app.Post("/meet/+/answer/delete", answerLimit, meetOps.deleteAnswer())

	// Poll lifecycle, admin link only: close and re-open stop and restore
	// answering, delete purges the poll and both of its links.
	app.Post("/meet/+/close", meetOps.closePoll())
	app.Post("/meet/+/reopen", meetOps.reopenPoll())
	app.Post("/meet/+/delete", meetOps.deletePoll())

	// 404 Handler
	app.Use(notFoundHandler)
}

// newApp wires up the template engine, middleware and routes. staticDir is
// the directory holding the templates and the css/favicon assets; callers may
// pass an absolute path to decouple the app from the working directory.
func newApp(staticDir string) *fiber.App {
	// We use html template engine
	engine := html.New(staticDir, ".html")

	// Dropdown labels come from zoneOptionLabels (the display name plus the
	// zone's standard UTC offset, e.g. "Europe/Berlin (UTC+1)"); option
	// values keep the real IANA name so the form still submits a loadable
	// zone. zoneOptionLabels precomputes for the curated list and falls back
	// to computing on demand for a zone the browser inserts that isn't in it.
	engine.AddFunc("zoneOption", func(z string) string {
		if l, ok := zoneOptionLabels[z]; ok {
			return l
		}
		return zoneOptionLabel(z)
	})

	// Template reload and parse logging are development conveniences, off by
	// default (enable with KIUSAN_DEV or the config file's dev key); reload
	// re-reads the templates from disk on every render.
	engine.Reload(cfg.DevMode)
	engine.Debug(cfg.DevMode)

	// Create app
	app := fiber.New(fiber.Config{
		Views:       engine,
		ReadTimeout: 10 * time.Second, // Don't keep cons alive for too long
		IdleTimeout: 60 * time.Second, // Drop idle keep-alive connections
	})

	// Logging middleware; the random link keys (capability URLs) are never
	// written to the log, so an exposed log can't be used to reach polls
	// or votes.
	app.Use(logger.New(logger.Config{
		Skip: func(c fiber.Ctx) bool {
			p := c.Path()
			return strings.HasPrefix(p, "/meet/") ||
				strings.HasPrefix(p, "/vote/") ||
				strings.HasPrefix(p, "/meet-form/edit/") ||
				strings.HasPrefix(p, "/vote-form/edit/")
		},
	}))

	// Set up all routes
	initRoutes(app, staticDir)

	return app
}

func main() {
	var err error
	cfg, err = loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Open the storage backend selected by KIUSAN_STORE; the DSN is built
	// from the config's db parts (Config.dsn) — there is no KIUSAN_DSN.
	var dsn string
	dsn, err = cfg.dsn()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	db, err = store.Open(cfg.Store, dsn)
	if err != nil {
		log.Fatalf("Could not open store: %s", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Println("Store Shutdown:", err)
		}
	}()

	// The maintenance loop: purges finished polls and votes once their
	// retention (KIUSAN_FINISHED_TTL, months after the earlier of
	// closure and end date) has passed, and checkpoints/compacts the
	// database.
	runJanitor(db, cfg.FinishedTTL)

	// Create app
	app := newApp("./static")

	// Shutting down section
	// Wait for interrupt signal to gracefully shutdown the server.
	// Must be registered before Listen, since Listen blocks.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	// Start the server and listen on http connections
	go func() {
		<-quit
		log.Println("Shutdown Server ...")
		// Shut down server
		if err := app.Shutdown(); err != nil {
			log.Println("Server stopped with error:", err)
		}
	}()

	if err := app.Listen(":" + strconv.Itoa(cfg.Port)); err != nil {
		log.Println("Server stopped with error:", err)
	}

	log.Println("Server exiting")
}
