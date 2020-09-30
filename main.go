package main

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/html"
	"github.com/recoilme/pudge"
)

type Meeting struct {
	Title       string
	Description string
	Email       string
	HideUsers   bool
	SingeSignup bool
	Dates       []time.Time
	Users       *Users
}

type Users struct {
	Name  string
	Dates []time.Time
}

func main() {
	// We use html template engine
	engine := html.New("./static", ".html")

	// For debugging, set to false or comment for production
	// Reload the templates on each render, good for development
	engine.Reload(true)

	// Debug will print each template that is parsed, good for debugging
	engine.Debug(true)

	// Create app
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	// Logging middleware
	app.Use(logger.New())

	// Make sure syle files are available
	app.Static("/", "./static/css")

	app.Get("/", homeHandler)
	app.Get("/1-1-form", formHandler)
	app.Post("/1-1-form", formPostHandler)

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
	form := new(Meeting)

	if err := c.BodyParser(form); err != nil {
		return c.Status(400).SendString(err.Error())
	}

	//// Feed form into database
	// Open database
	db, err := pudge.Open("meetings", nil)
	if err != nil {
		log.Printf("Error opening meetings database", err)
	}

	// Create random URL safe string for key
	bytes, _ := GenerateRandomBytes(12)
	urlKey := base64.URLEncoding.EncodeToString(bytes)

	pudge.Set("meetings", urlKey, &form)

	return c.Render("1-1-form", fiber.Map{
		"pagetitle": "kiusan | Meeting Form!",
	}, "layouts/home")
}

// This is adapted from https://gist.github.com/dopey/c69559607800d2f2f90b1b1ed4e550fb
// GenerateRandomBytes returns securely generated random bytes.
// It will return an error if the system's secure random
// number generator fails to function correctly, in which
// case the caller should not continue.
func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}

	return b, nil
}
