package main

import (
	"github.com/gofiber/fiber"
	"github.com/gofiber/fiber/middleware/logger"
	"github.com/gofiber/template/html"
)

// type OneForm struct {
// 	Title          string
// 	Description    string
// 	Email          string
// 	MultipleSignup bool
// }

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

	app.Get("/", func(c *fiber.Ctx) error {
		// Render index
		return c.Render("index", fiber.Map{
			"pagetitle": "kiusan | Hi!",
		}, "layouts/home")
	})

	_ = app.Listen(":8080")
}
