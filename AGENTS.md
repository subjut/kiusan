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
behind `store.Store` (`store/`, embedding `MeetingStore` + `VoteStore`),
backed by GORM on the relational engines (sqlite — the default — plus
mysql, mariadb, postgres), selected via `KIUSAN_STORE` + `KIUSAN_DSN`.
GORM schemas are auto-migrated at startup (additive-only).

Feature state (see `static/index.html`):
- "Schedule Meetings" — implemented end to end: the create/edit form
  (`/meet-form`), the two-link meeting page (respondent answer form
  with green/yellow marks — no "no" button; unmarked slots render red
  in the matrix —, submitted-answers matrix, admin link boxes, poll
  edit, answer management), the per-poll identity cookie with CSRF
  token, poll expiry, the prefilled poll edit that keeps both links,
  and the admin-side poll lifecycle (close / re-open / delete, with a
  dedicated 404 page for deleted links). The answers table ends with
  The answers table ends with
  the per-slot totals rows (`Yes`, plus `Yes + Maybe`) with the row's
  highest cells styled like the mark cells (green tint + text), the
  rows labelled a localized `Total` prefix + the mark's symbol (e.g.
  `Total ✓`, `Total ✓ (✓)`) — no totals on a single-signup poll, and
  none on the user page while respondents are hidden (the admin page
  keeps them).
- "Vote on Something" — implemented end to end: the create/edit form
  (`/vote-form`; title, contact email, description, up to 20 options
  with stable IDs, the four exclusive voting types — `yes/no` (the
  create default), `yes`, `yes/maybe/no`, and "voters may write a note
  per option" — the "pick only one option" and "must answer each
  option" checkboxes that grey out the voting types they are invalid
  with, and an end date), the two-link vote page (the form row + the
  submitted-answers matrix with mark toggles or per-option note
  inputs, the must-answer submit gate, answer edit/delete, admin
  answer management, the prefilled poll edit that keeps both links,
  and the admin lifecycle close / re-open / delete with the dedicated
  404 page for deleted links), the per-vote identity cookie with CSRF
token, and the per-choice totals rows per voting type (`Yes` always,
  `Yes + Maybe` + `Maybe` in yes/maybe/no, `No` in yes/no and
  yes/maybe/no; none for free text) with the row's highest cells
  with the row's highest cells styled like the mark cells (green /
  amber / red tint + text), the rows labelled `Total` + the mark's
  symbol — the admin page keeps them while respondents are hidden or
  the vote form's hide-totals option is on. The block is set off from
  the answer rows by a borderless gap row (the vote's totals carry no
  combined yes + maybe row).

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
- Test database: `go run ./tools/testdb -o test.db` recreates the seed
  sqlite file (six answer tables at every defined size cap — two 100-slot
  meetings x 100/50 respondents, four 20-option votes covering all voting
  types — 50-year end dates; it prints each table's admin + user links);
  run the server on it with `KIUSAN_DSN=./test.db go run .` (the DSN of a
  sqlite backend is just the data-file path).
- Environment: `KIUSAN_SECRET` (the CSRF secret — set it so tokens
  survive restarts), `KIUSAN_BASE_URL` (the public origin for the
  copyable link boxes; `https://` enables Secure cookies),
  `KIUSAN_DEV` (`1` enables template reload + parse logging).
- Tests: `go test ./...` (add `-race` in CI) — `smoke_test.go` runs the app
  in-process (no server, no network) and covers page rendering,
  favicon/static serving, the 404 fallthrough, a full meeting
  create -> redirect -> read round trip (timezone-aware slots), the form's
  timezone select (alphabetical options, the labels carrying the standard
  UTC offset in parentheses on the entity-decoded body), form validation
  (bad/malformed dates and times, invalid timezones, encoding), and the
  random URL key contract; `poll_test.go` covers the answer
  `poll_test.go` covers the answer
  lifecycle on the two-link model (identity cookie + CSRF, answering,
  visibility / hide-users, single-slot enforcement, the totals rows
  (placement below the form row, the max-cell highlight, the
  single-signup / hide-users visibility), answer edit/delete, admin
  answer management, poll edit with stable links and mark pruning,
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
  freetext modes), the totals rows per voting type (the per-type row
  set, the max-cell highlight, no rows on free text, the hide-users
  and hide-totals admin visibility, the hide-totals option stored +
  edit-preserved), and the edit semantics (stable option IDs,
  text-change / delete / reorder answer handling, the vote-type flip's
  mark/note pruning); `i18n_test.go` covers the locale resolution
  order (cookie → `Accept-Language` → English) and base-tag matching,
  the dropdown's selected state + `data-implicit` (cookie / header /
  default), the `/i18n/boot` + `/i18n/:lang` contract (merged dicts, the
  English fallback for unshipped tags, 404 for malformed tags), key
  parity between catalogs, and that every `data-i18n*` marker in the
  templates names a key the English catalog defines. The end dates the
  suites submit are derived from now (`futureEnd()` / `testEndDate()`
  in `smoke_test.go`) — a fixed calendar date would rot the suite, since
  `parseEndDate` rejects past end dates; the candidate slots stay fixed
  (a slot may be in the past, an end date may not), and the poll-D
  forced expiry is a derived past date the same way. `htmltest_test.go`
  holds the parser-based page assertions shared by the suites:
  `parsePage` (`golang.org/x/net/html`), `hasClass` / `elemsByClass`,
  `answersTable` + `tbodyRows` + `rowTds` + `nodeText`, `totalsRows`,
  `hotCells`, and `tableShape` (the answers table must stay rectangular
  — every row the same column span — the guard against stray injected
  closing tags); the totals-row assertions in `poll_test.go` and
  `vote_test.go` are structural on that tree, and `tableShape` runs on
  the smoke, poll, and vote page reads. `store/store_test.go` covers
  the GORM backend on an in-memory sqlite (the `Open` dispatch: empty
  kind → sqlite + the default data file, missing DSN, unknown kind;
  `Create`'s key minting, the two-link resolution, the field round
  trip, `Delete`; the CAS `Update` / `UpdateVote` — success bumps
  `Generation`, a stale stamp returns `store.ErrConflict`; plus
  AutoMigrate's idempotency on re-open), `database/database_test.go`
  the JSON-in-TEXT codec contract of the four list types (`TimeSlice`,
  `AnswerList`, `ChoiceList`, `VoteAnswerList`: round trips, nil →
  `[]`, malformed / mistyped `Scan` → error), and `helpers_test.go`
  table-driven unit tests for the pure helpers (`rowMax`, `slotKey`,
  `parseEndDate` / `endOf`, `matchLang`, `csrfToken`, `absURL`,
  `isFormPost`, `validVoteMark`, `assignChoiceIDs`, `pruneVoteAnswers`,
  `zoneLabel`, `offsetLabel` / `zoneOptionLabel` (the dropdown labels),
  `collectMarks` / `collectVoteAnswers` through a live form ctx via
  `probeForm`). The suites run on GORM-sqlite; each test chdirs into
  `t.TempDir()` and opens a throwaway store, so the dev data files are
  never touched. Coverage: main ~85%, store ~76%, database ~75%.
- CI: `.github/workflows/tests.yml` (push to main + PRs): `go build
  ./...`, `go vet ./...`, `go test -race` with an 80% total-coverage
  floor (currently ~84%) — the coverage run is scoped to the app
  packages (`go list ./... | grep -v '/tools/'`): the dev-only
  `tools/testdb` seed tool has no tests of its own, and a 0% package in
  the combined profile would drag the module total below the floor — and
  the floor check prints a message when it fails, `node --check` on
  every `static/js/*.js`, and the jstest npm suite — sqlite-only, no
  external services.
- JS tests: `npm --prefix jstest install && npm --prefix jstest test`
  (a standalone `node --test` package in `jstest/`) — the real
  `static/js/*` scripts are injected inline into jsdom in the browser's
  load order (locale.js from the header, page.js ahead of poll.js /
  vote.js, dater.js on the form page) against DOM fixtures mirroring the
  template markup, and cover: the dater picker (calendar rendering,
  select/deselect, past-day disabling, add-time-to-all, per-date times
  incl. Enter, chip removal, the clear buttons, the submit slot
  serialization, the seeded edit-mode prefill, the timezone prefill /
  edit-mode keep, the character counters, the single-signup wipe
  warning, the locale-switch re-render), the poll answers-table
  behaviour (the mark toggles, mark switching, the neutral unmarked
  state, the submitted-rows-untouched invariant, the single-slot
  grey-out incl. the disabled-button guard, the edit-row prefill, the
  sticky-header activation (the `table-sticky` class + the measured
  `--th-*` offset vars)), the vote answers-table behaviour (the per-type toggles, the yes vote's
  missing no-button, single-choice yes-exclusivity, the free-text
  note gate, the must-answer submit gate incl. the disabled-submit
  hint flash), and the locale display + dictionary swap (the
  cookie → browser resolution, the date-value contract per `data-*` key
  in the poll's zone, the UTC-offset annotation, the marker swaps,
  the `data-i18n-prefix` suffix keep, the select's effective-language
  sync, the dropdown fetch, the pin/un-pin cookie policy (pin on
  deviation from the implicit, delete on return to it), the
  failed-fetch no-op).

## Layout

- `main.go` — server setup only: the package-level `db` global
  (`store.Store`, opened in `main` via `store.Open`, env-selected),
  `loadConfig` (`KIUSAN_BASE_URL` / `KIUSAN_DEV` / secure-cookies),
  the per-client rate-limit consts, the embedded `data/timezones.txt`
  (loadable-filtered + sorted into `timezoneList` at process start),
  `zoneOptionLabels` (the dropdown option labels — the display name plus
  the standard non-DST UTC offset in parentheses, e.g.
  `Europe/Berlin (UTC+1)` — precomputed at process start), and the
  `zoneLabel` / `standardOffset` / `offsetLabel` / `zoneOptionLabel`
  helpers,
  `initRoutes` (the security-header middleware, the i18n endpoints, the
  four static-dir middlewares, the per-route rate limiters, and all
  page/form routes: `GET/POST /meet-form`, `GET /meet-form/edit/+`
  (admin key only), `GET/POST /vote-form`, `GET /vote-form/edit/+`,
  `GET /meet/+` + `POST /meet/+/answer|answer/edit|answer/delete` +
  the admin lifecycle `POST /meet/+/close|reopen|delete` (the
  answer/lifecycle routes register the shared `pollOps` builders —
  `meetOps.postAnswer()`, `voteOps.postAnswer()`, …), the same triple
  for `/vote/+`, and the 404 fallthrough), and `newApp`
  (template engine, the `zoneOption` template func (the precomputed
  option labels), the request logger whose
  `Skip` covers every capability URL so link keys never reach the log).
- `render.go` — the page-rendering plumbing: the `pageData` struct
  (the i18n base + `ThemeClass`, filled by `renderPage`, plus exactly
  one of four page pointers per render — `Meet` / `Vote` / `MeetForm` /
  `VoteForm` — the templates reach a page's data through it, e.g.
  `{{.Meet.Tz}}`, `{{.VoteForm.Choices}}`; both it and `answerPage`
  carry the `T` method) and `renderPage` (fills the i18n base, the
  theme class, and renders through `layouts/home`), `formBase` (the
  shared upper section of both create/edit forms — title, contact,
  description, hide-users, end date — embedded by `meetingForm` and
  `voteForm`), `answerPage` (the data shared by the meeting and vote
  pages — header meta, the admin links section, the answer form row,
  the table's aria strings — embedded by `meetingPage` and `votePage`
  and received by the shared partials, which call its `T` method as
  `{{.T "key"}}`), `themeClass`, `absURL` (the public
  origin for the copyable link boxes), `pressAttr`, the shared
  `totalsRow` type (mark label + tooltip word, color/st classes, the
  row's glyphs, per-column values, max) + `rowMax` (the answers
  tables' summary rows),
  `homeHandler`, the
  small response helpers (`notFoundHandler`, `linkGone` — the 404 page
  for a deleted poll or vote link, `static/meet-gone.html` rendered
  with a 404 status —, `sendErr`, `sendInternal`, `saveError`), the
  shared form size consts and `validateTextFields`, and
  `parseEndDate`/`endOf`.
- `answers.go` — the shared answer & lifecycle handlers for both link
  shapes: `answerData` (the type-erased submitted payload — per-slot /
  per-choice marks + the free-text notes), `pollOps[T]` (the
  type-specific surface of one answers table — `resolve` (user key
  first, then admin key), `exists`, `keyOf`, `cookie`, `ended` /
  `closed` / `setClosed`, `hasAns`, `collect`, `upsert` / `remove`,
  `save`, `purge`), the six generic handlers (`answerPost`,
  `answerEditPost`, `answerDeletePost`, `closePollPost`,
  `reopenPollPost`, `deletePollPost`) and the builder methods on
  `*pollOps[T]` (`postAnswer()` etc., each returning a `fiber.Handler`
  so the routes read `meetOps.postAnswer()`), and the package vars
  `meetOps` / `voteOps`.
- `meet-form.go` — the meeting form: the `meetingForm` page struct
  (embeds `formBase`; the timezone list, the prefill fields, the
  calendar labels), `meetFormHandler` (create), `meetFormEditHandler`
  (admin link only, prefilled, keeps both links), `extraTimezone`, and
  `meetFormPostHandler` (create or update-in-place on a `meetingkey`).
- `meet-page.go` — the meeting page: the table cell types
  (`formSlot`, `timeCell`, `headerYear`, `headerDate`, `respCol`), the
  `meetingPage` struct (embeds `answerPage`; the poll's zone, the slot
  grid, the respondent columns, the edit `FormCol`, the per-slot
  `Totals` rows), and
  `meetingHandler` (user key → respondent view, admin key → admin
  view). The answer & lifecycle endpoints are the shared generics in
  `answers.go`.
- `vote-form.go` — the vote form: the `voteForm` page struct (embeds
  `formBase`; the choices, the two mode checkboxes, the hide-totals
  checkbox, the vote type),
  `voteFormHandler` (create), `voteFormEditHandler` (admin link only,
  prefilled), `voteFormPostHandler` (create or update-in-place on a
  `votekey`).
- `vote-page.go` — the vote page: the `voteCol` cell type, the
  `votePage` struct (embeds `answerPage`; the mode, the choices, the
  vote columns, the edit `VoteFormCol`, the per-choice `Totals` rows),
  and `voteHandler`. The answer
  & lifecycle endpoints are the shared generics in `answers.go`.
- `poll.go` — the poll plumbing shared by the handlers: the
  per-identity cookie pair shared with the vote's (name-based
  `ensureIdentityCookie` / `dropIdentityCookie` — `kiusan-<userKey>`
  (see `cookieName`/`voteCookieName`), HttpOnly + SameSite=Lax, valid
  until the poll or vote ends; a closed one mints none, like an
  expired one), `isFormPost` (the urlencoded-only POST gate), the
  stateless CSRF token (HMAC-SHA256 over the cookie value, secret from
  `KIUSAN_SECRET` or random per start), `slotKey` (the slot's absolute
  UTC instant, seconds stripped), `resolveMeeting` (user key first,
  then admin key — the `pollOps` adapter's resolver), the
  answer-list operations (find/upsert/remove/prune), the generic
  `saveEntity` CAS round trip (fresh read, mutate, compare-and-set
  `Update`, retry on conflict), `loadMeeting`, the thin
  `saveMeeting` wrapper around it, and `collectMarks`.
- `vote.go` — the vote plumbing, mirroring the poll's: `voteCookieName`
  and `voteEnded`, `validVoteMark` (the marks a type accepts:
  `y` everywhere, `n` in yes-no/yes-maybe-no, `m` in yes-maybe-no
  only) and `collectVoteAnswers` (mark mode reads `a<ID>`; free-text
  mode reads `n<ID>`; must-answer rejects an unanswered option;
  single-choice allows at most one yes / one note), `assignChoiceIDs`
  (the options' stable IDs: an option keeps its ID only while its
  text is unchanged — a changed text is a new option whose answers are
  pruned, a deleted row drops its ID, reordering carries them),
  `pruneVoteAnswers` (drops marks/notes for removed options and the
  map the vote's type no longer uses, plus the `m` marks of a yes/no
  vote), `resolveVote` (the vote's `pollOps` resolver), `loadVote`, and
  the thin `saveVote` wrapper around `poll.go`'s generic `saveEntity`
  (compare-and-set `UpdateVote`, retry on conflict).
- `i18n.go` — the interface-localization plumbing: the `translator`
  (`gofiber/contrib/v3/i18n` over `go-i18n/v2` with a YAML
  `EmbedLoader` on the `locales/` directory), the `langHandler` hook
  (locale resolution: `kiusan-locale` cookie → `implicitLang` →
  English; `implicitLang` is the `Accept-Language` → English chain the
  select's `data-implicit` attribute carries), `matchLang` (base-tag
  matching so `de-CH` → `de`), the
  `/i18n/boot` handler (serves `window.I18N = {…}` as same-origin JS),
  and the `/i18n/:lang` handler (serves the JSON dict built live from
  the in-memory bundle with English fallback). The `pageData`,
  `formBase`, and `answerPage` structs (and their `T` methods) live in
  `render.go`.
- `database/database.go` — model types only: `Meeting` (incl. the IANA
  `TimeZone` (empty renders as UTC), the `uniqueIndex`ed `AdminKey`, the
  always-set `Expires`, `Closed` (a closed poll stops accepting
  answers, independent of `Expires`), `SingleSignup` (at most one slot
  per answer), `Answers`, and the CAS stamp `Generation`),
  `Answer`/`AnswerList` (submitted answers, marks keyed by `slotKey`;
  a JSON document in a TEXT column) and `TimeSlice` (candidate slots,
  same treatment). `database/vote.go` adds the vote types: `Vote`
  (incl. `VoteType` — `yes` / `yes-no` (the stored default) /
  `yes-maybe-no` / `freetext`, `SingleChoice` (valid only with `yes`
  and `freetext`), `MustAnswer` (valid only with `yes-no`,
  `yes-maybe-no`, and `freetext`; mutually exclusive with
  `SingleChoice`), and `HideTotals` (keeps the totals rows off the
  user page — the admin page keeps them)), `Choice`/`ChoiceList` (the
  options with their
  stable IDs), and `VoteAnswer`/`VoteAnswerList` (answers whose `Marks`
  and `Notes` maps are keyed by choice ID, exactly one of them
  populated per vote type), same JSON-in-TEXT treatment. No handles
  or globals here. `database_test.go` pins the codec contract of the
  four list types (see Commands → Tests).
- `store/` — `store.Store` (`store.go`, embedding `MeetingStore` +
  `VoteStore`) and its GORM-backed implementation `gorm.go` (sqlite —
  the default when `KIUSAN_STORE` is empty, DSN defaulting to
  `./meetings.db` — plus mysql/mariadb/postgres; schema auto-migrated at
  startup; the admin link is a `uniqueIndex`ed `admin_key` column).
  `Create` mints two unique link keys via the shared `createEntity`
  (collision-checked against both key columns, retry-capped);
  `Get`/`GetAdmin` resolve the two links; `Update` is a compare-and-set
  on `Generation` (`ErrConflict` when stale) and honors a changed admin
  key; the user key never changes. `VoteStore` (`vote.go`) carries the
  vote mirror — `CreateVote`, `GetVote`/`GetVoteAdmin`, and
  `UpdateVote`, the same compare-and-set. `Open(kind, dsn)` dispatches
  on `KIUSAN_STORE`; `RandomKey` in `key.go` builds the URL-safe keys.
  `store_test.go` pins the backend contract on an in-memory sqlite
  (see Commands → Tests). A new backend = implement `Store` + add a
  case in `Open`.
- `static/` — HTML templates rendered by gofiber/template/html/v3
  (`static/index.html`, `static/meet-form.html`, `static/meet.html`,
  `static/meet-gone.html` — the 404 page for a deleted poll or vote,
  `static/vote-form.html` — the vote create/edit form (incl. the
  `Voting type:` radio group and the hide-totals checkbox),
  `static/vote.html`; layout:
  `static/layouts/home.html`, partials in `static/partials/` — incl.
  `answers-table.html` (the poll's) and `vote-answers-table.html` (the
  shared answers-table markup the live vote form and the closed
  vote's read-only matrix both render; both end with the totals block
  — a borderless `tr.totals-gap` spacer row plus the `tr.totals-row`
  summary rows — below the form row), and `links.html` — the admin
  links section (copyable link boxes + copy buttons, the edit link,
  the close/re-open/delete actions with the inline two-step warning),
  rendered only in the admin view),
  plus `static/css/`, `static/js/` (the `dater.js` calendar/chip picker
  widget (locale-aware labels via `locale.js`), `poll.js` — the poll
  	`poll.js` — the poll
  page's answer-table toggling + single-slot grey-out, `page.js` — the
  shared page behaviour both page types have in common (the
  answers-table width measurement, the sticky-header row offsets —
  see Gotchas → Sticky table header —, the answer-delete confirmations,
  the two-step delete-poll/vote warning, the copy-box selection + copy
  buttons + link-box sizing; loaded ahead of `poll.js`/`vote.js` and
  providing their `t()`/`fill()` globals), `chooser.js` — the vote
  form's option rows (add/remove, reindex) and the mode interlock
  (single-choice greys out yes/no + yes/maybe/no and must-answer,
  must-answer greys out yes and single-choice; a selection on a greyed
  radio moves to the mode's default type), `vote.js` — the vote
  page's answers-table behavior (the mark toggles per voting type, the
  single-choice yes-exclusivity, the must-answer submit gate),
  `locale.js` — browser-locale date display: rewrites the
  server-rendered date labels (kept as the no-JS fallback) in the
  the viewer's locale and owns the site-header Display dropdown
  (`partials/locale-select.html` — the selected option is always the
  effective UI language, the select's `data-implicit` attribute
  carries the cookie-less `Accept-Language` language; see Gotchas →
  Interface localization for the pin/un-pin policy); loaded from
  `partials/header.html`,
  so it runs on every page, and `theme.js` — the light/dark toggle
  (`partials/theme-toggle.html`, a ghost button with a fixed
  sun–slash–moon icon beside the locale select; see Gotchas → Theme) —
  loaded from `partials/head.html`, ahead of `style.css` and ahead of
  `locale.js`; all vanilla, no deps),
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
  `.table-scroll` region with a visually-hidden `<caption>` — the
  region is the table's bounded vertical scroller (a `max-height`
  near the viewport) whose `thead` pins via `position: sticky`,
  activated by the `table-sticky` class `page.js` applies after
  measuring the header row heights (see Gotchas → Sticky table
  header).
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
  each label carrying the zone's standard (non-DST) UTC offset in
  parentheses — e.g. `Europe/Berlin (UTC+1)`, `Asia/Kolkata (UTC+5:30)` —
  computed in `main.go` from the zone itself, independent of the file
  order; embedded via `go:embed` and validated at process start.
  Hand-edit when the list should change.
- `jstest/` — the standalone npm package for the JS unit tests (see
  Commands → JS tests): `package.json` (the single dev dependency is
  jsdom), `harness.mjs` (the jsdom boot + load order: cookie / navigator
  / I18N seeds as head scripts ahead of the page scripts, the `settled`
  / `tick` / `click` helpers), and the four `*_test.mjs` suites
  (`locale_test.mjs`, `dater_test.mjs`, `poll_test.mjs`,
  `vote_test.mjs`), whose fixtures mirror the `static/` template
  markup. `node_modules/` is gitignored.
- `tools/testdb/` — the seed test-database generator (a `main` package,
  see Commands → Test database): it builds `test.db` through the app's
  own `store` package (same GORM schema, same key minting, so the file is
  production-exact) with the size-capped tables, the field-length caps
  (100-char titles, 256-char emails, 5000-char descriptions, 100-char
  option labels, 150-char notes, 50-char names) and a deterministic
  `rand`-seeded mark/notes distribution (slot/option 1 is all-marked,
  option 2 unanswered — both totals edge cases). Re-running overwrites
  the file (gitignored).
- Root test files: `smoke_test.go` (the shared `withStore` /
  `newTestApp` / `postForm` helpers + `futureEnd` / `testEndDate`),
  `htmltest_test.go` (the parser-based page assertions, see Commands →
  Tests), `helpers_test.go` (the pure-helper unit tests) and the other
  suites live in the root package; `.github/workflows/tests.yml` is the CI
  definition (see Commands → CI).
- `docs/prompts/` — verbatim log of user prompts, one file per date
  (the project's steering history across agent sessions); entries are
  appended automatically per prompt (see Conventions).
- `meetings.db` — the default runtime data file in the working directory
  (sqlite; gitignored); `test.db` — the generated seed test database
  (same treatment, see Commands → Test database).

## Conventions

- Go 1.26+ toolchain (go.mod declares `go 1.26.0`, required by the Fiber v3
  and template dependencies). Don't use stdlib or dependency features newer
  than that without bumping go.mod.
- All storage goes through the `db` global (`store.Store`); handlers
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
  alphabetical order (built into `timezoneList` at process start in
  `main.go`, independent of the file's grouping); `static/js/dater.js`
  inserts the browser's own zone at its value-alphabetical position when
  it is not in the list, so the prefill always shows and submits. The
  dropdown labels come from `zoneOptionLabel` (precomputed at startup
  into `zoneOptionLabels`, the `zoneOption` template function in
  `main.go`): the display name (underscores as spaces for locations,
  compact `UTC±N` for an `Etc/GMT` zone — the IANA sign is inverted:
  `Etc/GMT-5` is UTC+5) plus the zone's *standard (non-DST)* UTC offset in
  parentheses — `Europe/Berlin (UTC+1)`, `Asia/Kolkata (UTC+5:30)`, bare
  `UTC` stands alone — the standard offset being the smaller of the zone's
  January/July offsets (DST always advances the clock, true in both
  hemispheres). The poll page's `Timezone:` line uses the bare `zoneLabel`
  (no offset). Option values and stored data keep the real IANA name. Note
  html/template escapes the labels' `+` as `&#43;` in the rendered HTML
  (the browser decodes it; raw-string test assertions must unescape).
- **Locale display (client-side)**: date/time *values* show in the
  viewer's browser locale — `static/js/locale.js` rewrites the
  server-rendered labels (which stay as the no-JS fallback) with
  `Intl.DateTimeFormat`. Locale resolution: the `kiusan-locale` cookie
  (an unpersonalized BCP-47 tag, JS-only, 1-year `Lax`) wins, else
  `navigator.language`; the cookie is written by the site-header Display
  dropdown (`partials/locale-select.html`) only while a pick deviates
  from the device default and deleted while it returns to it (the full
  pin/un-pin policy is under Interface localization below). `locale.js` loads from
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
  the `dater.js`/`poll.js`/`vote.js` widget strings. Language switch: the
  dropdown `change` handler fetches `GET /i18n/<tag>` (JSON dict
  built live from the in-memory bundle, English fallback for missing
  keys) and calls `applyDict` — no page reload. The dropdown has no
  "Browser default" row: the selected option is always the effective UI
  language — the server renders `selected` on the option matching the
  resolved language (the same chain the page content uses) and carries
  the cookie-less implicit language (`Accept-Language` → shipped →
  English) in the select's `data-implicit` attribute; `locale.js`
  re-syncs the selection on load (cookie pin → browser language
  matched onto a shipped option → English, overwriting the
  server-rendered selection on a navigator/header divergence). A
  dropdown change writes the `kiusan-locale` pin only while the pick
  deviates from `data-implicit` and deletes it while it returns to the
  implicit language — a pin stores a deviation only. The initial dict is
  served by `GET /i18n/boot` as `window.I18N = {…}` (same-origin JS
  file, CSP `script-src 'self'` — no inline scripts). Catalogs are
  YAML in `locales/` (embedded, loaded at startup); maintenance =
  edit YAML + restart. Per-viewer language is intentional (no
  global setting). Base-tag auto-match: `de-CH` → `de`, `pt-BR` →
  `pt` (unshipped → English). The "never `{{` in YAML" rule prevents
  go-i18n template-action parse failures.
- **Theme (light/dark)**: no flash on load, and the toggle needs no
  reload. The `kiusan-theme` cookie (`dark`/`light`, 1-year, `Lax`,
  JS-only) is the viewer's explicit choice; `render.go`'s `themeClass`
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
- **Template engine** `Reload`/`Debug` are gated on `KIUSAN_DEV` —
  while set, template edits do not require a restart; without it the
  templates render as parsed at startup.
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
- **The edit form row is the table's last respondent row**: when
  editing an answer (`?edit=<id>`), the respondent's row is *not*
  rendered in place — it is skipped in the `{{range .Cols}}` loop and
  rendered as the table's last respondent row (prefilled from
  `meetingPage.FormCol`, the matching `respCol`), directly above the
  save/cancel bar and the totals rows (which stay the table's last
  rows), so long tables need no scrolling. Saving/canceling navigates
  to the plain page, where the row renders in its normal list position
  again; the move is a render-only choice, never persisted.
- **Two links, one route**: `meetingHandler` tries the user key, then the
  admin key, to pick the view. The admin link is a capability URL:
  admin-keyed endpoints (answer edit/delete, the poll edit form and its
  `meetingkey` field) are authorized by the key alone and carry no CSRF
  token; the cookie-identity endpoints (answering, deleting the own
  answer) require the `csrftoken` form field.
- **CSRF secret**: set `KIUSAN_SECRET` so tokens survive server restarts
  (still-open pages keep working); without it the secret is random per
  start and open pages must be reloaded after a restart.
- **Rate limiting & link keys in the log**: the unauthenticated write
  routes carry per-client limits (20 creates / 30 answers per minute,
  consts in `main.go`) on top of the per-poll size caps; the request
  logger's `Skip` covers every capability URL (`/meet/`, `/vote/`,
  `/meet-form/edit/`, `/vote-form/edit/`) so link keys never reach the
  log.
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
- **Deleted-poll 404**: a link key that resolves to no poll or vote
  renders `static/meet-gone.html` with status 404 (`linkGone`), for
  `GET /meet/+`, `GET /vote/+`, and the two form edit routes (`GET
  /meet-form/edit/+`, `GET /vote-form/edit/+`) — a full page,
  deliberately different from the plain-text 404 of an unknown path.
  Fiber v3's `c.Render` only sets the body and content type, so the
  404 status set on the ctx around it is the one that ships; POST
  answer/lifecycle endpoints keep the plain 404.
- **Totals rows**: both answers tables end with per-column summary
  rows (`tr.totals-row`, one cell per slot/choice, one row per counted
  mark): the meeting counts green (`Yes`) and green + yellow
  (`Yes + Maybe`) per slot; the vote counts per choice in the type's
  order (`Yes` always, `Maybe` in yes-maybe-no, `No` in yes-no +
  in yes-no + yes-maybe-no; free text has no marks, no rows — the vote's totals
  carry no combined yes + maybe row, only the meeting does). Each
  row's max cell(s) get the `.totals-hot` class + the answer cells'
  `st-*` styling (tint background + mark text color, via the
  precomputed `Mark`/`Cls` on `totalsRow` — attribute-value template
  actions may not carry quoted literals; the meeting's combined row
  is green); ties highlight together, an all-zero row highlights
  nothing, and non-max cells are dimmed to `--text-2` so the max
  stands out. The label cell is a localized `Total` prefix
  (`totals.total`) + the row's mark symbol in its mark color
  (`tot-g` / `tot-y` / `tot-r`: `✓`, `(✓)`, `✗`; the combined row
  carries `✓ (✓)`), the mark's word as a title tooltip (precomputed
  `Name`/`NameKey`, swapped on language change via `data-i18n-title` —
  html/template escapes the `+` in `Yes + Maybe` as `&#43;` in the
  attribute). The totals are the table's *last* rows — below the
  form row, which then sits second-to-last — set off from the answer
  rows by the borderless `tr.totals-gap` spacer (border-collapsed
  tables can't take row margins). Counts cover stored answers only (the form row's
  in-flight state is not part of it). Visibility: always on the admin
  page; on the user page they are hidden by a single-signup meeting,
  by `Vote.HideTotals` (the vote form's hide-totals option), or by
  hidden respondents on either page type.
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
- **Sticky table header (bounded scroll box)**: `.table-scroll` is the
  table's vertical scroller, too (`overflow: auto` + `max-height:
  clamp(320px, calc(100dvh − 18rem), 100dvh)`, `vh` fallback line), so a
  long table scrolls in place while the page keeps its natural height
  for everything else. The `thead` pins via `position: sticky` on the
  `th`s, one row at a time — each row's `top` is the sum of the rows
  above it, measured in `page.js` (the `--th-2` / `--th-3` CSS vars on
  the table; the date label wraps to 1–3 lines, so CSS can't know the
  offsets). Pinning activates only with the `table-sticky` class the
  same code applies after measuring — without JS every offset would be
  0px and the rows would stack on top of each other, so a no-JS browser
  keeps the plain page-scrolled table. `.answers-table` uses
  `border-collapse: separate` (`border-spacing: 0`) — collapse mode
  drops a sticky `thead`'s borders in Chrome; cells draw only their
  right + bottom borders, so nothing doubles and nothing is lost. The
  pinned `th`s take the opaque `--surface` background. The offsets are
  re-measured on table resize (the same `ResizeObserver` as the `--tw`
  width) and on `Locale.onChange` (a locale switch rewrites the date
  labels and can change a row's height). The meeting's `th-name` cell
  keeps its `rowspan="2|3"` — the spanning cell pins with the first row
  while the other rows pin at the exact offsets that tile it; if a
  browser misrenders the sticky span, the fallback is a per-row
  spacer cell in the templates instead of the rowspan.
