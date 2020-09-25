package main

import (
	"github.com/gofiber/fiber"
	"github.com/gofiber/fiber/middleware/logger"
	"github.com/gofiber/template/html"
)

type OneForm struct {
	Title          string
	Description    string
	Email          string
	MultipleSignup bool
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

	app.Get("/", func(c *fiber.Ctx) error {
		return c.Render("index", nil, "")
	})

	_ = app.Listen(":8080")

	// tmpl := template.Must(template.ParseFiles("./static/1-1-form.html"))

	// http.HandleFunc("/1-1-form1.html", func(w http.ResponseWriter, r *http.Request) {
	// 	if r.Method != http.MethodPost {
	// 		_ = tmpl.Execute(w, nil)
	// 		return
	// 	}

	// 	multipleSignup := false
	// 	if r.FormValue("1-multiple-signup") == "true" {
	// 		multipleSignup = true
	// 	}

	// 	details := OneForm{
	// 		Title:          r.FormValue("1-title"),
	// 		Email:          r.FormValue("1-email"),
	// 		Description:    r.FormValue("1-description"),
	// 		MultipleSignup: multipleSignup,
	// 	}

	// 	// do something with details
	// 	_ = details

	// 	_ = tmpl.Execute(w, struct{ Success bool }{true})
	// })

	// _ = http.ListenAndServe(":8080", nil)
}
