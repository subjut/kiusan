package main

import (
	"crypto/rand"
	"log"
	"math/big"
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

// Used for random URL generator
const urlSafeLetters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-"

const meetingDb = "meetings"
const userDb = "users"

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
		Views:       engine,
		ReadTimeout: 10 * time.Second, // Don't keep cons alive for too long
	})

	// Logging middleware
	app.Use(logger.New())

	// Make sure syle files are available
	app.Static("/", "./static/css")

	app.Get("/", homeHandler)
	app.Get("/1-1-form", formHandler)
	app.Post("/1-1-form", formPostHandler)
	app.Get("/meet/+", meetingDisplayHandler)

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

	// Get the form data
	if err := c.BodyParser(form); err != nil {
		return c.Status(400).SendString(err.Error())
	}

	//// Feed form into database
	// Open database
	db, err := pudge.Open(meetingDb, nil)
	if err != nil {
		log.Printf("Error opening meetings database", err)
	}

	// Create random URL safe string for key
	urlKey, _ := getRandomUrl(16)

	err = db.Set(urlKey, &form)
	if err != nil {
		return c.Status(500).SendString(err.Error())
	}

	// Redirect to meeting display
	return c.Redirect("/meet/" + urlKey)

	// return c.Render("1-1-form", fiber.Map{
	// 	"pagetitle": "kiusan | Meeting Form!",
	// }, "layouts/home")
}

func meetingDisplayHandler(c *fiber.Ctx) error {
	return c.SendString(c.Params("+"))
}

// getRandomUrl returns a secure and URL safe string of length length
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
