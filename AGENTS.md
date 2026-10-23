# AGENTS.md

## Project overview

Kiusan is a small self-hosted scheduling web app. A user creates a meeting
poll (title, contact email, description, one or more candidate dates) and
receives a random URL key to share: `/meet/<key>`.

Written in Go with the Fiber v3 web framework (v3.6.1). Storage is the embedded
Pudge key-value database — there is no SQL, no migrations, no ORM.

Feature state (see `static/index.html`):
- "Schedule Meetings" (1-1 form) — implemented (`/1-1-form`, `/meet/+`)
- "Vote on Something" — unimplemented (`static/choice-form.html` is a
  static stub, not templated and not routed)
- "Find Date" — unimplemented (`static/date-form.html`, same)

The `database.Users` type and `UserDb` constant exist, but no users DB is
opened and no sign-up endpoints exist yet — do not assume respondent
sign-up works.

## Commands

- Build: `go build ./...`
- Vet: `go vet ./...`
- Run: `go run .` (listens on `:8080`)
- Tests: `go test ./...` — `smoke_test.go` runs the app in-process (no server,
  no network) and covers page rendering, favicon/static serving, the 404
  fallthrough, and a full meeting create -> redirect -> read round trip.
  It uses the `./meetings` dev DB and deletes what it creates.

## Layout

- `main.go` — server setup, routes (`initRoutes`), handlers.
- `database/database.go` — Pudge handle (`database.DB`) and the
  `Meeting`/`Users` model types. New DBs must be opened in
  `main.initDbs`.
- `static/` — HTML templates rendered by gofiber/template/html/v3 (layout:
  `static/layouts/home.html`, partials in `static/partials/`), plus
  `static/css/` and `static/favicon/`, served from the site root via the
  static middleware: two `app.Get("/*", static.New(dir))` registrations in
  `initRoutes` (a file miss resets the response and calls `c.Next()`, so the
  second registration and later routes still run).
- `meetings`, `meetings.idx` — Pudge data files, generated at runtime in
  the working directory (gitignored).

## Conventions

- Go 1.26+ toolchain (go.mod declares `go 1.26.0`, required by the Fiber v3
  and template dependencies). Don't use stdlib or dependency features newer
  than that without bumping go.mod.
- Handlers stay thin. In Fiber v3 a handler is `func(c fiber.Ctx) error`
  (`Ctx` is an interface — no `*`). Form fields are bound into
  `database.Meeting` via `c.Bind().Body(form)`; candidate dates arrive as
  repeated form fields named `date0`, `date1`, ... and are bound separately.
  Redirects go through `c.Redirect().To(url)`. There is no `app.Static` any
  more — use the static middleware.
- Pages render with `c.Render("<name>", fiber.Map{...}, "layouts/home")`.

## Gotchas

- **Go time layouts**: HTML `<input type=date>` submits `YYYY-MM-DD`, so
  parse with layout `"2006-01-02"`. In Go's reference time `"01"` is the
  month and `"02"` is the day — swapping them silently produces zero
  times.
- **Catch-all routes**: Fiber v3 accepts both `+` (required, greedy) and `*`
  (optional, greedy) in route paths, and `c.Params("+")` / `c.Params("*")` are
  both valid lookups for a greedy param. Do not "normalize" `/meet/+` or the
  param lookup; note the static middleware uses the same wildcard form
  (`/*`), registered with `Get`, not `app.Static`.
- **Pudge data files** (`meetings`, `meetings.idx`) are generated at
  runtime — never hand-edit, delete, or commit them.
- **Template engine** runs with `Reload(true)` and `Debug(true)` for
  development — template edits do not require a restart.
- **Slices before indexing**: `meeting.Dates` must be checked for empty
  before indexing (handlers must not panic on malformed stored data).
