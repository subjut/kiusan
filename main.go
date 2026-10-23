package main

import (
	"crypto/rand"
	"log"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/favicon"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/gofiber/template/html/v3"
	"github.com/recoilme/pudge"

	"github.com/subjut/kiusan/database"
)

// Used for random URL generator
const urlSafeLetters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-"

// initDbs initializes all databases used in kiusan
func initDbs() {
	var err error
	database.DB, err = pudge.Open(database.MeetingDb, nil)
	if err != nil {
		log.Fatalf("Could not initialize databases: %s", err)
	}
}

func initRoutes(app *fiber.App, staticDir string) {
	// Make sure syle files and favicons are available
	// (served from the site root; a file miss resets the response and calls Next)
	app.Get("/*", static.New(filepath.Join(staticDir, "css")))
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
	// Initialize databases
	initDbs()
	defer pudge.CloseAll()

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

	// Closing all open dbs
	if err := pudge.CloseAll(); err != nil {
		log.Fatal("Database Shutdown:", err)
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
		"today":     time.Now().Format("2006-01-02"),
	}, "layouts/home")
}

func formPostHandler(c fiber.Ctx) error {
	form := new(database.Meeting)

	//// Get the form data
	// General fields
	if err := c.Bind().Body(form); err != nil {
		return c.Status(400).SendString(err.Error())
	}

	// Dates and times
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
		form.Dates = append(form.Dates, newDate)
	}

	//// Feed form into database
	// Make sure we don't have it yet
	var urlKey string
	for newKeyNotfound := true; newKeyNotfound; {
		// Create random URL safe string for key
		var err error
		urlKey, err = getRandomUrl(16)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}

		dbHasKey, err := database.DB.Has(urlKey)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		if !dbHasKey {
			newKeyNotfound = false
		}
	}

	err := database.DB.Set(urlKey, &form)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	// Redirect to meeting display
	return c.Redirect().To("/meet/" + urlKey)
}

func meetingDisplayHandler(c fiber.Ctx) error {
	urlKey := c.Params("+")

	// Check if the meeting is in the database
	dbHasKey, err := database.DB.Has(urlKey)
	if err != nil || !dbHasKey {
		return notFoundHandler(c)
	}

	// Get meeting info from db
	meeting := new(database.Meeting)
	err = database.DB.Get(urlKey, &meeting)
	if err != nil {
		return notFoundHandler(c)
	}

	// Show all offered dates, one per line
	lines := []string{meeting.Title, meeting.Email, meeting.Description}
	for _, date := range meeting.Dates {
		lines = append(lines, date.Local().String())
	}
	return c.SendString(strings.Join(lines, "\n"))
}

// getRandomUrl returns a secure and URL safe string of length length.
// Returns an empty string and error if the random number generator crashes
// adapted from https://gist.github.com/denisbrodbeck/635a644089868a51eccd6ae22b2eb800
func getRandomUrl(length int) (string, error) {
	result := ""
	for {
		if len(result) >= length {
			return result, nil
		}
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(urlSafeLetters))))
		if err != nil {
			return "", err
		}
		result = result + string(urlSafeLetters[num.Int64()])
	}
}
