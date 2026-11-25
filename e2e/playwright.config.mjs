// Playwright config for the Kiusan e2e suite: one real server (started by
// Playwright's webServer hook with a throwaway data dir, so the dev data
// files are never touched) and one headless Chromium (installed by the
// devcontainer's Dockerfile; `npx playwright install chromium` once on a
// bare host).
import { defineConfig } from "@playwright/test";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.join(here, "..");

export default defineConfig({
  testDir: here,
  testMatch: "*.spec.mjs",
  timeout: 30_000,
  // The suite is tiny and every test shares the one server.
  workers: 1,
  use: {
    baseURL: "http://127.0.0.1:8080",
  },
  webServer: {
    command: "go run .",
    cwd: repoRoot,
    url: "http://127.0.0.1:8080/",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    env: {
      // A throwaway data dir per machine: the suite never reads or writes
      // the developer's meetings.db, and the config file the app persists
      // lands in the same temp dir.
      KIUSAN_DATA_DIR: path.join(os.tmpdir(), "kiusan-e2e"),
      KIUSAN_PORT: "8080",
      KIUSAN_SECRET: "e2e-only-secret",
    },
  },
});
