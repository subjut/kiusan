package main

import (
	_ "embed"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
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

// Runtime configuration, read once from the environment in loadConfig.
// devMode enables template reload + parse logging (off by default);
// baseURL is the public origin (KIUSAN_BASE_URL) for the copyable link
// boxes, empty falls back to the request's scheme+host; secureCookies sets
// the Secure cookie flag, derived from baseURL being https.
var (
	devMode       bool
	baseURL       string
	secureCookies bool
)

// Per-client rate limits on the unauthenticated write routes.
const (
	rateWindow         = time.Minute
	createMaxPerMinute = 20
	answerMaxPerMinute = 30
)

// loadConfig reads the runtime configuration from the environment.
func loadConfig() {
	baseURL = strings.TrimRight(os.Getenv("KIUSAN_BASE_URL"), "/")
	secureCookies = strings.HasPrefix(baseURL, "https://")
	switch strings.ToLower(os.Getenv("KIUSAN_DEV")) {
	case "1", "true", "yes", "on":
		devMode = true
	}
}

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

	// Dropdown labels show zone names with underscores as spaces
	// ("America/New York"); option values keep the real IANA name so
	// the form still submits a loadable zone.
	engine.AddFunc("zoneDisplay", func(z string) string {
		return strings.ReplaceAll(z, "_", " ")
	})

	// Template reload and parse logging are development conveniences, off by
	// default (enable with KIUSAN_DEV); reload re-reads the templates from
	// disk on every render.
	engine.Reload(devMode)
	engine.Debug(devMode)

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
	loadConfig()

	// Open the storage backend selected by KIUSAN_STORE / KIUSAN_DSN.
	var err error
	db, err = store.Open(os.Getenv("KIUSAN_STORE"), os.Getenv("KIUSAN_DSN"))
	if err != nil {
		log.Fatalf("Could not open store: %s", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Println("Store Shutdown:", err)
		}
	}()

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

	if err := app.Listen(":8080"); err != nil {
		log.Println("Server stopped with error:", err)
	}

	log.Println("Server exiting")
}
