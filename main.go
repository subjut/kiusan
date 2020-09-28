package main

import (
	"github.com/gofiber/fiber"
	"github.com/gofiber/fiber/middleware/logger"
	"github.com/gofiber/template/html"
)

type MeetingForm struct {
	Title       string
	Description string
	Email       string
	HideUsers   bool
	SingeSignup bool
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

	_ = app.Listen(":8080")
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
	form := new(MeetingForm)

	if err := c.BodyParser(form); err != nil {
		return c.Status(400).SendString(err.Error())
	}

	return c.Render("1-1-form", fiber.Map{
		"pagetitle": "kiusan | Meeting Form!",
	}, "layouts/home")
}
