# Dev Container

This is a development container for Kiusan, based on the official [Go image](https://mcr.microsoft.com/devcontainers/go). Node.js is included as well for syntax checking the JavaScript files (e.g.`node --check static/js/poll.js`).

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
