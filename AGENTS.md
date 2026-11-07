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
  two-link meeting page (respondent answer form with green/yellow
  marks — no "no" button; unmarked slots render red in the matrix —,
  submitted-answers matrix, admin link boxes, poll edit, answer
  management), the per-poll identity cookie with CSRF token, poll
  expiry, the prefilled poll edit that keeps both links, and the
  admin-side poll lifecycle (close / re-open / delete, with a
  dedicated 404 page for deleted links).
- "Vote on Something" — implemented end to end: the create/edit form
  (title, contact email, description, up to 20 options with stable
  IDs, the four exclusive voting types — `yes/no` (the create
  default), `yes`, `yes/maybe/no`, and "voters may write a note per
  option" — the "pick only one option" and "must answer each option"
  checkboxes that grey out the voting types they are invalid with, and
  an end date), the two-link vote page (the form row + the
  submitted-answers matrix with mark toggles or per-option note inputs,
  the must-answer submit gate, answer edit/delete, admin answer
  management, the prefilled poll edit that keeps both links, and the
  admin lifecycle close / re-open / delete with the dedicated 404 page
  for deleted links), the per-vote identity cookie with CSRF token.
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
  expiry and prolongation, the admin poll lifecycle (close / re-open /
  delete, incl. the dedicated 404 page for deleted links), the
  clickjacking headers, and the urlencoded-only POST gate;
  `vote_test.go` covers the vote form (render, create, the vote-type
  and field validation incl. the mode-checkbox combinations, the
  pre-checked edit form), the vote page per voting type (render, the
  single-choice rules, the neutral unmarked cells of a plain yes/no
  vote), the answer round trips (incl. the type-specific rejections:
  no `n` mark in `yes`, no `m` outside yes/maybe/no, max one yes / note
  in single-choice, the must-answer completeness 400 in mark and
  freetext modes), and the edit semantics (stable option IDs,
  text-change / delete / reorder answer handling, the vote-type flip's
  mark/note pruning). The suites
  run on GORM-sqlite; each test chdirs into `t.TempDir()` and opens a
  throwaway store, so the dev data files are never touched.

## Layout

- `main.go` — server setup, app wiring (`newApp`), routes (`initRoutes`,
  incl. the clickjacking-header middleware and the poll routes: `GET
  /1-1-form/edit/+` (admin key only), `POST /meet/+/answer`,
  `POST /meet/+/answer/edit`, `POST /meet/+/answer/delete`, and the
  admin lifecycle `POST /meet/+/close|reopen|delete`), and the
  thin handlers — incl. `meetingGone`, the 404 page for a deleted poll
  (`static/meet-gone.html` rendered with a 404 status). The store is
  opened in `main` via `store.Open` (env-selected); handlers reach it
  through the package-level `db` global.
- `poll.go` — the poll plumbing shared by the handlers: the per-poll
  identity cookie (`kiusan-<userKey>`, HttpOnly + SameSite=Lax, valid
  until the poll ends; a closed poll mints none, like an expired one),
  the
  stateless CSRF token (HMAC-SHA256 over the cookie value, secret from
  `KIUSAN_SECRET` or random per start), `slotKey` (the slot's absolute
  UTC instant, seconds stripped), the answer-list operations
  (find/upsert/remove/prune), and `saveMeeting` (fresh read, mutate,
  compare-and-set `Update`, retry on conflict).
- `vote.go` — the vote plumbing, mirroring the poll's: the per-vote
  identity cookie (`kiusan-<userKey>`, minted until the vote ends; a
  closed vote mints none), `validVoteMark` (the marks a type accepts:
  `y` everywhere, `n` in yes-no/yes-maybe-no, `m` in yes-maybe-no
  only) and `collectVoteAnswers` (mark mode reads `a<ID>`; free-text
  mode reads `n<ID>`; must-answer rejects an unanswered option;
  single-choice allows at most one yes / one note), `assignChoiceIDs`
  (the options' stable IDs: an option keeps its ID only while its
  text is unchanged — a changed text is a new option whose answers are
  pruned, a deleted row drops its ID, reordering carries them),
  `pruneVoteAnswers` (drops marks/notes for removed options and the
  map the vote's type no longer uses, plus the `m` marks of a yes/no
  vote), and `saveVote` (fresh read, mutate, compare-and-set
  `UpdateVote`, retry on conflict).
- `i18n.go` — the interface-localization plumbing: the `translator`
  (`gofiber/contrib/v3/i18n` over `go-i18n/v2` with a YAML
  `EmbedLoader` on the `locales/` directory), the `langHandler` hook
  (locale resolution: `kiusan-locale` cookie → `Accept-Language` →
  English), `matchLang` (base-tag matching so `de-CH` → `de`), the
  `/i18n/boot` handler (serves `window.I18N = {…}` as same-origin JS),
  and the `/i18n/:lang` handler (serves the JSON dict built live from
  the in-memory bundle with English fallback). The `pageData` struct
  and its `T` method live in `main.go`.
- `database/database.go` — model types only: `Meeting` (incl. the IANA
  `TimeZone` (empty renders as UTC), the `uniqueIndex`ed `AdminKey`, the
  always-set `Expires`, `Closed` (a closed poll stops accepting
  answers, independent of `Expires`), `Answers`, and the CAS stamp
  `Generation`),
  `Answer`/`AnswerList` (submitted answers, marks keyed by `slotKey`;
  a JSON document in a TEXT column) and `TimeSlice` (candidate slots,
  same treatment). `database/vote.go` adds the vote types: `Vote`
  `database/vote.go` adds the vote types: `Vote`
  (incl. `VoteType` — `yes` / `yes-no` (the stored default) /
  `yes-maybe-no` / `freetext`, `SingleChoice` (valid only with `yes`
  and `freetext`), and `MustAnswer` (valid only with `yes-no`,
  `yes-maybe-no`, and `freetext`; mutually exclusive with
  `SingleChoice`)), `Choice`/`ChoiceList` (the options with their
  stable IDs), and `VoteAnswer`/`VoteAnswerList` (answers whose `Marks`
  and `Notes` maps are keyed by choice ID, exactly one of them
  populated per vote type), same JSON-in-TEXT treatment. No handles
  or globals here.
- `store/` — the `MeetingStore` interface and its GORM-backed
  implementation `gorm.go` (sqlite — the default when `KIUSAN_STORE` is
  empty, DSN defaulting to `./meetings.db` — plus mysql/mariadb/postgres;
  schema auto-migrated at startup; the admin link is a `uniqueIndex`ed
  `admin_key` column). `Get`/`GetAdmin` resolve the two links;
  `Get`/`GetAdmin` resolve the two links;
  `Update` is a compare-and-set on `Generation` (`ErrConflict` when stale)
  and honors a changed admin key; the user key never changes.
  `VoteStore` (`vote.go`) carries the vote mirror —
  `GetVote`/`GetVoteAdmin`, `CreateVote`, and `UpdateVote`, the same
  compare-and-set. `Open(kind, dsn)` dispatches on `KIUSAN_STORE`;
  `RandomKey` in `key.go` builds the URL-safe keys. A new backend =
  implement `MeetingStore` + `VoteStore` + add a case in `Open`.
- `static/` — HTML templates rendered by gofiber/template/html/v3
  (`static/index.html`, `static/1-1-form.html`, `static/meet.html`,
  `static/meet-gone.html` — the 404 page for a deleted poll,
  `static/choice-form.html` — the vote create/edit form (incl. the
  `Voting type:` radio group), `static/vote.html`; layout:
  `static/layouts/home.html`, partials in `static/partials/` — incl.
  `answers-table.html` (the poll's) and `vote-answers-table.html`, the
  shared answers-table markup the live vote form and the closed
  vote's read-only matrix both render),
  plus `static/css/`, `static/js/` (the `dater.js` calendar/chip picker
  widget (locale-aware labels via `locale.js`), `poll.js` — answer-table
  toggling, single-slot grey-out, answer-delete confirmations, the
  two-step delete-poll warning, copy-box
  selection, `chooser.js` — the vote form's option rows (add/remove,
  reindex) and the mode interlock (single-choice greys out yes/no +
  yes/maybe/no and must-answer, must-answer greys out yes and
  single-choice; a selection on a greyed radio moves to the mode's
  default type), `vote.js` — the vote page's answers-table behavior
  (the mark toggles per voting type, the single-choice yes-
  exclusivity, the must-answer submit gate, the delete confirmations),
  `locale.js` —
  browser-locale date display: rewrites the
  server-rendered date labels (kept as the no-JS fallback) in the
  viewer's locale and owns the site-header Display dropdown
  (`partials/locale-select.html`); loaded from `partials/header.html`,
  so it runs on every page, and `theme.js` — the light/dark toggle
  (`partials/theme-toggle.html`, a ghost button with a fixed
  sun–slash–moon icon beside the locale select; see Gotchas → Theme) —
  loaded from
  `partials/head.html`, ahead of `style.css` and ahead of `locale.js`; all
  vanilla, no deps),
  `static/fonts/` (the self-hosted Inter variable font
  `InterVariable.woff2` + its OFL `LICENSE.txt`, served as
  `/InterVariable.woff2`) and `static/favicon/`, all served from the
  site root via the static middleware: one
  `app.Get("/*", static.New(dir))` registration per dir in `initRoutes`
  (a file miss resets the response and calls `c.Next()`, so later
  registrations and routes still run).
  `static/css/style.css` is the design system: all colors are
  `:root` custom properties (warm light theme the `:root` default, plus
  a warm dark theme that re-targets the same tokens — see Gotchas →
  Theme), a single teal accent, semantic green/amber/red answer marks
  (dark glyph on light tint in light mode, light glyph on dark tint in
  dark mode), organized as `@layer theme, base, components`, with a
  global `:focus-visible` ring, `prefers-reduced-motion` and
  `forced-colors` support, and 44px calendar-day targets. Templates
  carry the a11y structure: `lang="en"`, a skip link, one `h1`
  (the wordmark) with `h2` page/card titles, a `<main id="main">`
  landmark, and the answers table wrapped in a focusable
  `.table-scroll` region with a visually-hidden `<caption>`.
- `locales/` — the i18n YAML catalogs (`en.yaml` is the source of
  truth, `de.yaml` is the German translation; one file per language).
  Embedded via `go:embed` in `i18n.go` and loaded at process start.
  To add a language: create `<lang>.yaml` with the same keys (or a
  subset — missing keys fall back to English) and add the tag to
  `AcceptLanguages` in `i18n.go`. To update a translation: edit the
  YAML and restart. Never use `{{...}}` in values (go-i18n treats it
  as a template action); client-side placeholders use single braces
  (`{name}`, `{date}`).
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
  `database.Meeting` via `c.Bind().Body(form)`; candidate slots arrive as
  repeated form fields named `slot0`, `slot1`, ... (each `YYYY-MM-DD` for
  a midnight slot or `YYYY-MM-DDTHH:MM` for a timed slot — times are
  per date, see `dater.js`) and are bound separately. Redirects go
  through `c.Redirect().To(url)`. There is no `app.Static` any more —
  use the static middleware.
- Pages render with `c.Render("<name>", &pageData{...}, "layouts/home")`
  where `pageData` is a typed struct (exported fields + a `T` method
  that calls `translator.MustLocalize`). Templates reference fields as
  `{{.FieldName}}` (or `{{$.FieldName}}` inside `{{range}}`) and
  localized strings as `{{.T "key"}}` (or `{{$.T "key"}}`).
- **Re-read templates after writing them**: `write_file` has mangled
  nested HTML in this project several times — the call succeeds, but the
  file ends up with stray closing tags injected right after their opens
  (`<thead></thead>`, `<tr></tr>`, `</tbody>` right after `<tbody>`,
  `</td>` right after `<td>`). The result looks plausible at a glance and
  some tests won't catch it. After every `write_file` (or any large
  `edit_file`) of a `.html` file, re-read the written file and check that
  the tag nesting is balanced. If a template looks broken, re-derive the
  markup from `git show HEAD:<path>` (or the nearest known-good commit)
  instead of patching it by guesswork.
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
  `Europe/Berlin`) is where they are authored and rendered, empty
  renders as UTC. Never display with `.Local()` — that leaks the server's
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
  not in the list, so the prefill always shows and submits. Labels show
  the zone name with underscores as spaces (the `zoneDisplay` template
  function in `main.go`, also applied to the poll page's `Timezone:`
  line); option values and stored data keep the real IANA name.
- **Locale display (client-side)**: date/time *values* show in the
  viewer's browser locale — `static/js/locale.js` rewrites the
  server-rendered labels (which stay as the no-JS fallback) with
  `Intl.DateTimeFormat`. Locale resolution: the `kiusan-locale` cookie
  (an unpersonalized BCP-47 tag, JS-only, 1-year `Lax`, set by the
  site-header Display dropdown in `partials/locale-select.html`; curated
  ~15 locales shown by native name + the browser's own language
  auto-added) wins, else `navigator.language`. `locale.js` loads from
  the header (before the page body), so its initial reformat waits for
  `DOMContentLoaded`. Every value is formatted with the poll's zone —
  the page container's `data-tz` (IANA name, `UTC` when the poll has
  none); the viewer's own zone is never used for the values. The
  contract: `data-slot` (a `slotKey` instant) on the year/date/time
  header cells, `data-lines` (1|2|3) on date cells, `data-slot` +
  `data-style` (`medium`|`long`) on the `f-date` expiry spans, and
  `data-base` on the `tz-name` span (the client appends the UTC offset,
  `longOffset` form e.g. `GMT+1`). The form's calendar (`dater.js`)
  formats its wall-clock labels through `Locale.dtf` (no `timeZone`),
  follows the locale's first weekday best-effort
  (`resolvedOptions().firstDay`, Sunday fallback) and re-renders on
  locale change. UI strings are localized via the i18n system below.
- **Interface localization (i18n)**: UI strings are server-rendered in
  the viewer's language (no-JS stays localized) and switchable
  client-side without a reload (half-filled forms survive). The
  `kiusan-locale` cookie (the same one that drives date-value
  formatting) is the single locale setting: it drives both the UI
  language (server-side, via the `LangHandler` hook: cookie →
  `Accept-Language` → English) and the date-value locale (client-side,
  `Intl.DateTimeFormat`). The `<html lang>` attribute and the page
  `<title>` are localized. Templates carry `data-i18n` (text content),
  `data-i18n-ph` (placeholder), `data-i18n-title` (title attr),
  `data-i18n-aria` (aria-label), `data-i18n-meta` (meta content), and
  `data-i18n-prefix` (title prefix, keeps `data-suffix` = user's poll
  title) markers. The client swapper (`locale.js` `applyDict`) uses
  only `textContent`/`setAttribute` — no innerHTML — and re-renders
  the `dater.js`/`poll.js` widget strings. Language switch: the
  dropdown `change` handler fetches `GET /i18n/<tag>` (JSON dict
  built live from the in-memory bundle, English fallback for missing
  keys) and calls `applyDict` — no page reload. The initial dict is
  served by `GET /i18n/boot` as `window.I18N = {…}` (same-origin JS
  file, CSP `script-src 'self'` — no inline scripts). Catalogs are
  YAML in `locales/` (embedded, loaded at startup); maintenance =
  edit YAML + restart. Per-viewer language is intentional (no
  global setting). Base-tag auto-match: `de-CH` → `de`, `pt-BR` →
  `pt` (unshipped → English). The "never `{{` in YAML" rule prevents
  go-i18n template-action parse failures.
- **Theme (light/dark)**: no flash on load, and the toggle needs no
  reload. The `kiusan-theme` cookie (`dark`/`light`, 1-year, `Lax`,
  JS-only) is the viewer's explicit choice; `main.go`'s `themeClass`
  reads it in `renderPage` and `static/layouts/home.html` renders it as
  a class on `<html>` (`theme-dark`/`theme-light`, no class when the
  cookie is absent). The CSS dark theme is the same `:root` tokens
  re-targeted (two identical blocks in `style.css` — edit them
together): one under `@media (prefers-color-scheme: dark)` scoped to
  `:root:not(.theme-light)`, one scoped to `:root.theme-dark`. Without
  the cookie the media query follows the system setting (the default);
  `static/js/theme.js` (loaded in `partials/head.html`) then follows
  live system changes while no cookie is set, and the header toggle
  the header toggle
  (`partials/theme-toggle.html`, beside the locale select) flips the
  theme, mirrors it onto the same classes, and persists it to the
  cookie. The toggle's icon is one fixed symbol (sun top-left,
  diagonal slash, moon bottom-right — all `currentColor`, always
  visible; only its color changes with the theme, no icon swapping),
  and its localized `aria-label` is managed by `theme.js` from
  `window.I18N` (not a `data-i18n-aria` marker) and re-applied on
  language changes via `Locale.onChange`. `meta theme-color` in `partials/head.html`
  colors the mobile browser chrome per system scheme.
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
- **Marks: no "no" button**: the form cells offer only the green / yellow
  toggles; "no" is the *absence* of a mark — `collectMarks` accepts
  `""`, `g`, `y` only, and the stored answer map keeps no `r`. Unmarked
  form cells render neutral (no `st-r`; `respCol.Fcls` maps unmarked to
  an empty class), while the submitted-answers rows keep the red tint +
  ✗ (`respCol.Csls`). Toggling a mark off clears it to the neutral
  state. The small legend above the table (`meet.legend`) documents the
  glyphs, which are rendered at 1.25rem inside the fixed 44px columns.
- **The edit form row is the table's last row**: when editing an answer
  (`?edit=<id>`), the respondent's row is *not* rendered in place — it is
  skipped in the `{{range .Cols}}` loop and rendered as the table's last
  row (prefilled from `pageData.FormCol`, the matching `respCol`),
  directly above the save/cancel bar, so long tables need no scrolling.
  Saving/canceling navigates to the plain page, where the row renders in
  its normal list position again; the move is a render-only choice,
  never persisted.
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
  timezone; the form's `enddate` is optional and defaults to three
  months, so every poll has one. An ended poll answers with 410, mints
  no new identity cookie, and hides the form — but the admin page keeps
  its edit button, so the organizer can prolong the poll and reopen
  answering.
- **Poll lifecycle (close / re-open / delete)**: `Meeting.Closed` is
  independent of `Expires`. A closed poll answers like an expired one
  (410 on all three answer endpoints, no new identity cookie, no form
  row), but both links keep rendering the answers table as a static
  matrix (the shared `partials/answers-table.html`; the row
  edit/delete icons render only while the poll is open). An expired
  (not closed) poll keeps the old display: the notice only, no table.
  The admin page shows "Close poll" while the poll is open, "Re-open
  poll" while it is closed, and always a "Delete poll" button that
  opens an inline warning (`role=alert`, a JS-gated two-step confirm —
  the confirm button is the only submit path). The lifecycle routes are
  capability URLs (admin key only, no CSRF) like the other admin
  endpoints. Delete purges the row — with every answer and its stored
  identity — and expires the requesting browser's identity cookie in
  the redirect to `/`.
- **Deleted-poll 404**: a link key that resolves to no poll renders
  `static/meet-gone.html` with status 404 (`meetingGone`), for `GET
  /meet/+` and `GET /1-1-form/edit/+` — a full page, deliberately
  different from the plain-text 404 of an unknown path. Fiber v3's
  `c.Render` only sets the body and content type, so the 404 status set
  on the ctx around it is the one that ships; POST answer/lifecycle
  endpoints keep the plain 404.
- **Voting types & the mode checkboxes**: `Vote.VoteType` is `yes`
  (a yes-toggle only — there is no no-button, unselected is neutral),
  `yes-no` (the stored default), `yes-maybe-no`, or `freetext`. Two
  checkboxes shape it, each valid only with specific types:
  `SingleChoice` (max one yes / one note) with `yes` and `freetext` —
  the form greys the other radios out and moves the selection to
  `yes`; `MustAnswer` (every option needs a mark/note before submit)
  with `yes-no`, `yes-maybe-no`, and `freetext` — it greys out `yes`
  (which would force a yes on every option), and the vote page keeps
  the submit button disabled until the answer is complete. The two
  checkboxes disable each other (vice versa). `voteFormPostHandler`
  and `collectVoteAnswers` re-check every combination (400), and
  `validVoteMark` restricts the accepted marks per type (`n` not in
  `yes`, `m` not outside `yes-maybe-no`). An unmarked option is
  neutral in every type — there is no implicit no anywhere (the old
  single-choice "rest become no" normalization is gone). A vote-type
  edit re-shapes stored answers via `pruneVoteAnswers`: to `yes` drops
  the `n`/`m` marks, to `yes-no` drops the `m` marks, to freetext
  drops the marks, from freetext drops the notes.
