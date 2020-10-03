package main

import (
	"crypto/rand"
	"log"
	"math/big"
	"os"
	"os/signal"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/html"
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

func initRoutes(app *fiber.App) {
	// Make sure syle files and favicons are available
	app.Static("/", "./static/css")
	app.Static("/", "./static/favicon")

	// Normal routes
	app.Get("/", homeHandler)
	app.Get("/1-1-form", formHandler)
	app.Post("/1-1-form", formPostHandler)

	// Meeting page route; gobbles up random meeting url
	app.Get("/meet/+", meetingDisplayHandler)

	// 404 Handler
	app.Use(func(c *fiber.Ctx) error {
		return c.SendStatus(404) // => 404 "Not Found"
	})
}

func main() {
	// Initialize databases
	initDbs()
	defer pudge.CloseAll()

	// We use html template engine
	engine := html.New("./static", ".html")

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
		File: "./static/favicon/favicon.ico",
	}))

	// Logging middleware
	app.Use(logger.New())

	// Set up all routes
	initRoutes(app)

	// Start the server and listen to http connections
	_ = app.Listen(":8080")

	// Shutting down section
	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Println("Shutdown Server ...")
	// Shut down server
	app.Shutdown()

	// Closing all open dbs
	if err := pudge.CloseAll(); err != nil {
		log.Fatal("Database Shutdown:", err)
	}

	log.Println("Server exiting")
}

func notFoundHandler(c *fiber.Ctx) error {
	return c.SendStatus(404)
}

func homeHandler(c *fiber.Ctx) error {
	return c.Render("index", fiber.Map{
		"pagetitle": "kiusan | Hi!",
	}, "layouts/home")
}

func formHandler(c *fiber.Ctx) error {
	return c.Render("1-1-form", fiber.Map{
		"pagetitle": "kiusan | Meeting Form!",
	}, "layouts/home")
}

func formPostHandler(c *fiber.Ctx) error {
	form := new(database.Meeting)

	// Get the form data
	if err := c.BodyParser(form); err != nil {
		return c.Status(400).SendString(err.Error())
	}

	//// Feed form into database
	// Make sure we don't have it yet
	var urlKey string
	for newKeyNotfound := true; newKeyNotfound; {
		// Create random URL safe string for key
		urlKey, _ = getRandomUrl(16)

		dbHasKey, _ := database.DB.Has(urlKey)
		if !dbHasKey {
			newKeyNotfound = false
		}
	}

	err := database.DB.Set(urlKey, &form)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	// Redirect to meeting display
	return c.Redirect("/meet/" + urlKey)
}

func meetingDisplayHandler(c *fiber.Ctx) error {
	urlKey := c.Params("+")

	// Check if the meeting is in the database
	dbHasKey, _ := database.DB.Has(urlKey)
	if !dbHasKey {
		_ = notFoundHandler(c)
		return nil
	}

	// Get meeting info from db
	meeting := new(database.Meeting)
	err := database.DB.Get(urlKey, &meeting)
	if err != nil {
		_ = notFoundHandler(c)
		return nil
	}

	return c.SendString(meeting.Title + "\n" + meeting.Email + "\n" + meeting.Description + "\n")
}

// getRandomUrl returns a secure and URL safe string of length Descriptionngth

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
