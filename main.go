package main

import (
	"html/template"
	"net/http"
)

type OneForm struct {
	Title          string
	Description    string
	Email          string
	MultipleSignup bool
}

func main() {
	tmpl := template.Must(template.ParseFiles("./static/1-1-form.html"))

	http.HandleFunc("/1-1-form1.html", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			_ = tmpl.Execute(w, nil)
			return
		}

		multipleSignup := false
		if r.FormValue("1-multiple-signup") == "true" {
			multipleSignup = true
		}

		details := OneForm{
			Title:          r.FormValue("1-title"),
			Email:          r.FormValue("1-email"),
			Description:    r.FormValue("1-description"),
			MultipleSignup: multipleSignup,
		}

		// do something with details
		_ = details

		_ = tmpl.Execute(w, struct{ Success bool }{true})
	})

	_ = http.ListenAndServe(":8080", nil)
}
