# AGENTS.md

## Project overview

Kiusan is a small self-hosted scheduling web app. A user creates a meeting
poll (title, contact email, description, candidate dates with optional
times in a per-meeting timezone, and an end date) and gets two random
URL links on the same route: a respondent link `/meet/<key>` to answer
the poll, and an admin link `/meet/<adminKey>` to manage it (the
organizer answers through it too). Respondents are identified by a
per-poll browser cookie — there are no user accounts.

Written in Go with the Fiber v3 web framework (v3.6.1). Storage is pluggable
behind `store.MeetingStore` (`store/`), backed by GORM on the relational
engines (sqlite — the default — plus mysql, mariadb, postgres), selected
via `KIUSAN_STORE` + `KIUSAN_DSN`. GORM schemas are auto-migrated at
startup (additive-only).

Feature state (see `static/index.html`):
- "Schedule Meetings" — implemented end to end: the 1-1 form, the
  two-link meeting page (respondent answer form with green/yellow/red
  marks, submitted-answers matrix, admin link boxes, poll edit, answer
  management), the per-poll identity cookie with CSRF token, poll
  expiry, and the prefilled poll edit that keeps both links.
- "Vote on Something" — unimplemented (`static/choice-form.html` is a
  static stub, not templated and not routed)
- "Find Date" — unimplemented (`static/date-form.html`, same)

## Commands

- Build: `go build ./...`
- Vet: `go vet ./...`
- Run: `go run .` (listens on `:8080`, default sqlite storage in
  `./meetings.db`)
- Other backends:
  - `KIUSAN_STORE=sqlite KIUSAN_DSN=./other.db go run .` (a non-default sqlite file)
  - `KIUSAN_STORE=mysql KIUSAN_DSN="user:pass@tcp(localhost:3306)/kiusan" go run .`
    (mariadb: same DSN with `KIUSAN_STORE=mariadb`)
  - `KIUSAN_STORE=postgres KIUSAN_DSN="postgres://user:pass@localhost:5432/kiusan?sslmode=disable" go run .`
- Tests: `go test ./...` (add `-race` in CI) — `smoke_test.go` runs the app
  in-process (no server, no network) and covers page rendering,
  favicon/static serving, the 404 fallthrough, a full meeting
  create -> redirect -> read round trip (timezone-aware slots), form
  validation (bad/malformed dates and times, invalid timezones, encoding),
  and the random URL key contract; `poll_test.go` covers the answer
  lifecycle on the two-link model (identity cookie + CSRF, answering,
  visibility / hide-users, single-slot enforcement, answer edit/delete,
  admin answer management, poll edit with stable links and mark pruning,
  legacy polls without an admin key, expiry and prolongation, the
  clickjacking headers, and the urlencoded-only POST gate). The suites
  run on GORM-sqlite; each test chdirs into `t.TempDir()` and opens a
  throwaway store, so the dev data files are never touched.

## Layout

- `main.go` — server setup, app wiring (`newApp`), routes (`initRoutes`,
  incl. the clickjacking-header middleware and the poll routes: `GET
  /1-1-form/edit/+` (admin key only), `POST /meet/+/answer`,
  `POST /meet/+/answer/edit`, `POST /meet/+/answer/delete`), and the
  thin handlers. The store is opened in `main` via `store.Open`
  (env-selected); handlers reach it through the package-level `db`
  global.
- `poll.go` — the poll plumbing shared by the handlers: the per-poll
  identity cookie (`kiusan-<userKey>`, HttpOnly + SameSite=Lax, valid
  until the poll ends — 90 days for legacy polls without an end), the
  stateless CSRF token (HMAC-SHA256 over the cookie value, secret from
  `KIUSAN_SECRET` or random per start), `slotKey` (the slot's absolute
  UTC instant, seconds stripped), the answer-list operations
  (find/upsert/remove/prune), and `saveMeeting` (fresh read, mutate,
  compare-and-set `Update`, retry on conflict).
- `database/database.go` — model types only: `Meeting` (incl. the
  optional IANA `TimeZone`, the nullable `AdminKey` — empty = legacy, no
  admin link — `Expires`, `Answers`, and the CAS stamp `Generation`),
  `Answer`/`AnswerList` (submitted answers, marks keyed by `slotKey`;
  a JSON document in a TEXT column) and `TimeSlice` (candidate slots,
  same treatment). No handles or globals here.
- `store/` — the `MeetingStore` interface and its GORM-backed
  implementation `gorm.go` (sqlite — the default when `KIUSAN_STORE` is
  empty, DSN defaulting to `./meetings.db` — plus mysql/mariadb/postgres;
  schema auto-migrated at startup; the admin link is a `uniqueIndex`ed,
  nullable `admin_key` column). `Get`/`GetAdmin` resolve the two links;
  `Update` is a compare-and-set on `Generation` (`ErrConflict` when stale)
  and honors a changed admin key; the user key never changes.
  `Open(kind, dsn)` dispatches on `KIUSAN_STORE`; `RandomKey` in `key.go`
  builds the URL-safe keys. A new backend = implement `MeetingStore` +
  add a case in `Open`.
- `static/` — HTML templates rendered by gofiber/template/html/v3
  (`static/index.html`, `static/1-1-form.html`, `static/meet.html`;
  layout: `static/layouts/home.html`, partials in `static/partials/`),
  plus `static/css/`, `static/js/` (the `dater.js` calendar/chip picker
  widget and `poll.js` — answer-table toggling, single-slot grey-out,
  delete confirmations, copy-box selection; both vanilla, no deps) and
  `static/favicon/`, served from the site root via
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
- `meetings.db` — the default runtime data file in the working directory
  (sqlite; gitignored).

## Conventions

- Go 1.26+ toolchain (go.mod declares `go 1.26.0`, required by the Fiber v3
  and template dependencies). Don't use stdlib or dependency features newer
  than that without bumping go.mod.
- All storage goes through the `db` global (`store.MeetingStore`); handlers
  never touch GORM directly, and GORM is imported only inside `store/`.
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
- **Per-backend data files** (sqlite: `meetings.db`; relational: the
  GORM-created table in the DB) are generated at runtime — never
  hand-edit or commit them.
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
- **Template action quotes & range root**: never put quoted string
  literals inside `{{...}}` actions that sit inside an attribute value —
  html/template mis-tokenizes and the parse failure surfaces somewhere
  odd ("in attribute name"); precompute such values in the handler.
  Inside a nested `{{range}}` the page data root is `$.`, not `.` (the
  loop variable shadows it).
- **Checkbox binding**: the schema binder accepts only `on` or a
  parseable bool for `bool` fields, so the form's checkboxes use
  `value="true"` and the handler reads them via
  `c.FormValue(name) != ""` instead of the binder — an unparseable
  checkbox value 400s the whole submission.
- **The end-date field is `enddate`**: deliberately not named after
  `Meeting.Expires` — the binder would feed a `YYYY-MM-DD` value into
  `*time.Time` (RFC3339-only `TextUnmarshaler`) and 400 the form.
- **Answers mark instants, not rows**: marks are keyed by
  `slotKey(slot)` (the slot's absolute UTC instant, seconds stripped), so
  editing the poll (adding/removing slots) never shifts stored marks;
  `pruneAnswers` drops marks for slots that no longer exist, and both
  links survive every edit.
- **Two links, one route**: `meetingHandler` tries the user key, then the
  admin key, to pick the view. The admin link is a capability URL:
  admin-keyed endpoints (answer edit/delete, the poll edit form and its
  `meetingkey` field) are authorized by the key alone and carry no CSRF
  token; the cookie-identity endpoints (answering, deleting the own
  answer) require the `csrftoken` form field.
- **CSRF secret**: set `KIUSAN_SECRET` so tokens survive server restarts
  (still-open pages keep working); without it the secret is random per
  start and open pages must be reloaded after a restart.
- **Expiry**: `Expires` is end-of-day (23:59:59) in the meeting's
  timezone; nil = legacy = no end. An ended poll answers with 410, mints
  no new identity cookie, and hides the form — but the admin page keeps
  its edit button, so the organizer can prolong the poll and reopen
  answering.
- **Legacy polls**: an empty `AdminKey` (stored NULL on the relational
  backends — the column must stay nullable: a unique index over
  `''` would break with two or more legacy rows) means no admin view:
  the key never matches, and the user page simply offers no poll edit.
