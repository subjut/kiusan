package main

import (
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	// Bundle Go's zoneinfo so time.LoadLocation works on hosts without
	// system tzdata (Alpine, minimal containers).
	_ "time/tzdata"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/favicon"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/gofiber/template/html/v3"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// db is the storage backend for the whole app. Handlers talk to storage
// only through it; tests swap it for a throwaway instance per run.
var db store.MeetingStore

func initRoutes(app *fiber.App, staticDir string) {
	// Make sure syle files and favicons are available
	// (served from the site root; a file miss resets the response and calls Next)
	app.Get("/*", static.New(filepath.Join(staticDir, "css")))
	app.Get("/*", static.New(filepath.Join(staticDir, "js")))
	app.Get("/*", static.New(filepath.Join(staticDir, "favicon")))

	// Normal routes
	app.Get("/", homeHandler)
	app.Get("/1-1-form", formHandler)
	app.Post("/1-1-form", formPostHandler)

	// Meeting page route; gobbles up random meeting url
	app.Get("/meet/+", meetingDisplayHandler)

	// 404 Handler
	app.Use(func(c fiber.Ctx) error {
		return c.SendStatus(404) // => 404 "Not Found"
	})
}

// newApp wires up the template engine, middleware and routes. staticDir is
// the directory holding the templates and the css/favicon assets; callers may
// pass an absolute path to decouple the app from the working directory.
func newApp(staticDir string) *fiber.App {
	// We use html template engine
	engine := html.New(staticDir, ".html")

	// For debugging, set to false or comment for production
	// Reload the templates on each render, good for development
	engine.Reload(true)

	// Debug will print each template that is parsed, good for debugging
	engine.Debug(true)

	// Create app
	app := fiber.New(fiber.Config{
		Views:       engine,
		ReadTimeout: 10 * time.Second, // Don't keep cons alive for too long
	})

	// Serve favicon.ico from memory instead of disk
	app.Use(favicon.New(favicon.Config{
		File: filepath.Join(staticDir, "favicon", "favicon.ico"),
	}))

	// Logging middleware
	app.Use(logger.New())

	// Set up all routes
	initRoutes(app, staticDir)

	return app
}

func main() {
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
	signal.Notify(quit, os.Interrupt)

	// Start the server and listen to http connections
	go func() {
		<-quit
		log.Println("Shutdown Server ...")
		// Shut down server
		if err := app.Shutdown(); err != nil {
			log.Println("Server Shutdown:", err)
		}
	}()

	if err := app.Listen(":8080"); err != nil {
		log.Println("Server stopped with error:", err)
	}

	log.Println("Server exiting")
}

func notFoundHandler(c fiber.Ctx) error {
	return c.SendStatus(404)
}

func homeHandler(c fiber.Ctx) error {
	return c.Render("index", fiber.Map{
		"pagetitle": "kiusan | Hi!",
	}, "layouts/home")
}

func formHandler(c fiber.Ctx) error {
	return c.Render("1-1-form", fiber.Map{
		"pagetitle": "kiusan | Meeting Form!",
	}, "layouts/home")
}

func formPostHandler(c fiber.Ctx) error {
	form := new(database.Meeting)

	//// Get the form data
	// General fields
	if err := c.Bind().Body(form); err != nil {
		return c.Status(400).SendString(err.Error())
	}

	// Timezone the meeting's dates/times are authored and displayed in;
	// optional, empty (legacy) renders as UTC.
	form.TimeZone = strings.TrimSpace(c.FormValue("timezone"))
	loc, err := time.LoadLocation(form.TimeZone)
	if err != nil {
		return c.Status(400).SendString("Invalid timezone: " + form.TimeZone)
	}

	// Dates
	var dates []time.Time
	for i := 0; true; i++ {
		date := c.FormValue("date" + strconv.Itoa(i))
		if date == "" {
			break
		}

		// HTML date inputs submit YYYY-MM-DD
		newDate, err := time.Parse("2006-01-02", date)
		if err != nil {
			return c.Status(400).SendString("Invalid date: " + date)
		}
		dates = append(dates, newDate)
	}

	// Times, each propagated to every selected date.
	var times []time.Time
	for i := 0; true; i++ {
		t := c.FormValue("time" + strconv.Itoa(i))
		if t == "" {
			break
		}
		newTime, err := time.Parse("15:04", t)
		if err != nil {
			return c.Status(400).SendString("Invalid time: " + t)
		}
		times = append(times, newTime)
	}

	//// Feed form into database
	// Candidate slots = dates × times; without times, dates stay at
	// midnight (behavior pinned by the form test).
	for _, d := range dates {
		if len(times) == 0 {
			form.Dates = append(form.Dates, d)
			continue
		}
		for _, t := range times {
			form.Dates = append(form.Dates,
				time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, loc))
		}
	}

	urlKey, err := db.Create(form)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	// Redirect to meeting display
	return c.Redirect().To("/meet/" + urlKey)
}

func meetingDisplayHandler(c fiber.Ctx) error {
	urlKey := c.Params("+")

	// Check if the meeting is in the database
	meeting, err := db.Get(urlKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return notFoundHandler(c)
		}
		return c.Status(500).SendString(err.Error())
	}

	// All slots are shown in the meeting's timezone (UTC when none was
	// set) so the display never depends on the server's timezone.
	loc, _ := time.LoadLocation(meeting.TimeZone)
	tzName := meeting.TimeZone
	if tzName == "" {
		tzName = "UTC"
	}

	// Show all offered slots, one per line
	lines := []string{meeting.Title, meeting.Email, meeting.Description, "Timezone: " + tzName}
	for _, date := range meeting.Dates {
		lines = append(lines, date.In(loc).Format("2006-01-02 15:04"))
	}
	return c.SendString(strings.Join(lines, "\n"))
}
