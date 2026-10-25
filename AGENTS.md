# AGENTS.md

## Project overview

Kiusan is a small self-hosted scheduling web app. A user creates a meeting
poll (title, contact email, description, candidate dates with optional
times in a per-meeting timezone) and receives a random URL key to share:
`/meet/<key>`.

Written in Go with the Fiber v3 web framework (v3.6.1). Storage is pluggable
behind `store.MeetingStore` (`store/`): the embedded Pudge key-value database
is the default, and GORM backs the relational engines (mysql, mariadb,
postgres, sqlite), selected via `KIUSAN_STORE` + `KIUSAN_DSN`. GORM schemas
are auto-migrated at startup (additive-only).

Feature state (see `static/index.html`):
- "Schedule Meetings" (1-1 form) — implemented (`/1-1-form`, `/meet/+`)
- "Vote on Something" — unimplemented (`static/choice-form.html` is a
  static stub, not templated and not routed)
- "Find Date" — unimplemented (`static/date-form.html`, same)

The `database.Users` type exists (excluded from the schema via
`gorm:"-"`) but no sign-up endpoints exist yet — do not assume respondent
sign-up works.

## Commands

- Build: `go build ./...`
- Vet: `go vet ./...`
- Run: `go run .` (listens on `:8080`, Pudge default storage)
- Other backends:
  - `KIUSAN_STORE=sqlite KIUSAN_DSN=./meetings.db go run .`
  - `KIUSAN_STORE=mysql KIUSAN_DSN="user:pass@tcp(localhost:3306)/kiusan" go run .`
    (mariadb: same DSN with `KIUSAN_STORE=mariadb`)
  - `KIUSAN_STORE=postgres KIUSAN_DSN="postgres://user:pass@localhost:5432/kiusan?sslmode=disable" go run .`
- Tests: `go test ./...` (add `-race` in CI) — `smoke_test.go` runs the app
  in-process (no server, no network) and covers page rendering,
  favicon/static serving, the 404 fallthrough, a full meeting
  create -> redirect -> read round trip (timezone-aware slots), form
  validation (bad/malformed dates and times, invalid timezones, encoding),
  and the random URL key contract. The suites run on
  both backends (Puge and GORM-sqlite); each test chdirs into `t.TempDir()`
  and opens a throwaway store, so the dev data files are never touched.

## Layout

- `main.go` — server setup, app wiring (`newApp`), routes (`initRoutes`),
  handlers. The store is opened in `main` via `store.Open` (env-selected);
  handlers reach it through the package-level `db` global.
- `database/database.go` — model types only: `Meeting` (incl. the
  optional IANA `TimeZone`), `Users`, and `TimeSlice` (candidate slots as
  a JSON document in a TEXT column, so the schema stays portable across
  engines). No handles or globals here.
- `store/` — `MeetingStore` interface and backends: `puge.go` (embedded
  Pudge KV) and `gorm.go` (mysql/mariadb/postgres/sqlite via GORM; schema
  auto-migrated at startup). `Open(kind, dsn)` dispatches on `KIUSAN_STORE`;
  `RandomKey` in `key.go` builds the URL-safe keys. A new backend =
  implement `MeetingStore` + add a case in `Open`.
- `static/` — HTML templates rendered by gofiber/template/html/v3 (layout:
  `static/layouts/home.html`, partials in `static/partials/`), plus
  `static/css/`, `static/js/` (the `dater.js` calendar/chip picker widget,
  vanilla, no deps) and `static/favicon/`, served from the site root via
  the static middleware: one `app.Get("/*", static.New(dir))` registration
  per dir in `initRoutes` (a file miss resets the response and calls
  `c.Next()`, so later registrations and routes still run).
- `data/timezones.txt` — the IANA zones offered in the form's timezone
  select (a curated list, grouped by UTC offset for maintainers: the 1-2
  most important zones per offset per region, plus UTC; `#` lines are
  comments). Rendered alphabetically in the form (sorted in `main.go`),
  independent of the file order; embedded via `go:embed` and validated at
  process start. Hand-edit when the list should change.
- `docs/prompts/` — verbatim log of user prompts, one file per date
  (the project's steering history across agent sessions); entries are
  appended automatically per prompt (see Conventions).
- `meetings`, `meetings.idx` (Puge default), `meetings.db` (sqlite) —
  runtime data files in the working directory (gitignored).

## Conventions

- Go 1.26+ toolchain (go.mod declares `go 1.26.0`, required by the Fiber v3
  and template dependencies). Don't use stdlib or dependency features newer
  than that without bumping go.mod.
- All storage goes through the `db` global (`store.MeetingStore`); handlers
  never touch Pudge or GORM directly, and GORM/Pudge are imported only
  inside `store/`.
- Handlers stay thin. In Fiber v3 a handler is `func(c fiber.Ctx) error`
  (`Ctx` is an interface — no `*`). Form fields are bound into
  `database.Meeting` via `c.Bind().Body(form)`; candidate dates arrive as
  repeated form fields named `date0`, `date1`, ... and are bound separately.
  Redirects go through `c.Redirect().To(url)`. There is no `app.Static` any
  more — use the static middleware.
- Pages render with `c.Render("<name>", fiber.Map{...}, "layouts/home")`.
- **Prompt logging (automatic)**: after processing every user prompt,
  **before** sending the summary of what was done, append an entry to
  `docs/prompts/<YYYY-MM-DD>.md` (one file per date; when a new day
  starts, create it with the `# Prompt log` header + conventions
  section of an existing file): the prompt quoted verbatim (typos and
  all), numbered sequentially within the file, followed by a one-line
  note of what it produced or decided. No asking, no skipping.

## Gotchas

- **Go time layouts**: HTML `<input type=date>` submits `YYYY-MM-DD`
  (layout `"2006-01-02"`) and `<input type=time>` submits `HH:MM` (layout
  `"15:04"`). In Go's reference time `"01"` is the month and `"02"` is the
  day — swapping them silently produces zero times.
- **Catch-all routes**: Fiber v3 accepts both `+` (required, greedy) and `*`
  (optional, greedy) in route paths, and `c.Params("+")` / `c.Params("*")` are
  both valid lookups for a greedy param. Do not "normalize" `/meet/+` or the
  param lookup; note the static middleware uses the same wildcard form
  (`/*`), registered with `Get`, not `app.Static`.
- **Per-backend data files** (Puge: `meetings` + `meetings.idx`; sqlite:
  `meetings.db`; relational: the GORM-created table in the DB) are generated
  at runtime — never hand-edit or commit them.
- **Portable columns only**: the schema must stay engine-agnostic. Dates
  persist via `database.TimeSlice` as a JSON document in a TEXT column —
  don't add engine-specific column types or features.
- **Meeting timezone & server-independence**: slots are absolute instants
  (RFC3339 in `TimeSlice`); `Meeting.TimeZone` (IANA name, e.g.
  `Europe/Berlin`) is where they are authored and rendered, empty = legacy
  = render as UTC. Never display with `.Local()` — that leaks the server's
  timezone. `_ "time/tzdata"` in `main.go` bundles zoneinfo for hosts
  without system tzdata. The form's timezone `<select>` is rendered from
  the committed `data/timezones.txt` (embedded with `go:embed`, filtered
  `time.LoadLocation` at process start into `timezoneList` in
  `main.go`, so any name the server cannot load is dropped with a
  startup warning), so every option is loadable by the server — don't
  hardcode zone options in the template. The options are presented in
  alphabetical order (sorted into `timezoneList` at process start in
  `main.go`, independent of the file's grouping); `static/js/dater.js`
  inserts the browser's own zone at its alphabetical position when it is
  not in the list, so the prefill always shows and submits.
- **AutoMigrate is additive-only**: it creates tables and adds columns at
  startup but does not drop or modify anything; destructive schema changes
  need explicit SQL migrations (none exist yet).
- **Template engine** runs with `Reload(true)` and `Debug(true)` for
  development — template edits do not require a restart.
- **Slices before indexing**: `meeting.Dates` must be checked for empty
  before indexing (handlers must not panic on malformed stored data).
