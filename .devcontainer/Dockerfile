# Dev container image for Kiusan: the official Go image plus the tools the
# dev/test loop needs — a stable Node LTS (the jsdom suite + Playwright),
# the sqlite3 CLI (inspecting the data files), golangci-lint (a deeper Go
# check than go vet), and Playwright with Chromium (real-browser E2E).
FROM mcr.microsoft.com/devcontainers/go

# Stable Node LTS (the Go base image ships none) + the sqlite3 CLI.
RUN apt-get update && apt-get install -y --no-install-recommends \
        curl sqlite3 \
    && curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# golangci-lint into the image's GOPATH (whose bin is on the PATH).
ENV GOPATH=/home/vscode/go
RUN go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0

# Playwright + Chromium. The browsers live in a shared path so the
# non-root container user finds them at runtime.
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
RUN npm install -g playwright@1.65.1 \
    && npx playwright install --with-deps chromium
