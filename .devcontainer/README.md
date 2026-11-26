# Dev Container

This is a development container for Kiusan. It is built from the [`.devcontainer/Dockerfile`](Dockerfile) on top of the official [Go image](https://mcr.microsoft.com/devcontainers/go) and adds the tools the dev/test loop needs:

- **Node.js LTS** (24) — the jsdom suite (`jstest/`) and the Playwright e2e suite (`e2e/`); also `node --check` for the JavaScript files.
- **sqlite3 CLI** — inspecting the data files (`meetings.db`, `test.db`).
- **golangci-lint** — a deeper Go check than `go vet` (which is what CI runs): `golangci-lint run`.
- **actionlint** — the GitHub Actions workflow lint (`.github/workflows/`): `actionlint`.
- **Playwright + Chromium** — the browser for the e2e suite. The Chromium binary lives in `/ms-playwright` (the `PLAYWRIGHT_BROWSERS_PATH` set by the image) so the non-root container user finds it.

## Opening it

In VS Code, run **Dev Containers: Reopen in Container** from the command palette. From a terminal, use `devcontainer up .`.

## What is pre-configured

- **Port 8080** is forwarded, since that is where the app listens by default.
- **`KIUSAN_SECRET`** is set to a fixed development value. Without it, the app picks a random secret on every start, which invalidates the
  CSRF tokens of any page that is already open after a restart. The hardcoded value is fine for local use; set a real one if you share the container.

## Running the app

```sh
go run .
```

The app listens on `:8080` and stores its data in `./meetings.db` (sqlite) by default. Open [http://localhost:8080](http://localhost:8080) in your browser to manually test it. To run the test suite run `go test ./...`; the tests currently don't use the `./meetings.db` file.

## The test suites

- **Go** (in-process, no server): `go test ./...`
- **JS unit** (jsdom against the real `static/js/*`): `npm --prefix jstest install && npm --prefix jstest test`
- **E2E** (real server + headless Chromium): `npm --prefix e2e install && npm --prefix e2e test` — the Playwright config starts the server itself (`go run .` on `:8080`) with a throwaway `KIUSAN_DATA_DIR`, so it never touches your data files. On a host without the devcontainer, install the browser once with `npx --prefix e2e playwright install chromium`.
