// Playwright config for the Kiusan e2e suite: one real server (started by
// Playwright's webServer hook with a throwaway data dir, so the dev data
// files are never touched) and one headless Chromium (installed by the
// devcontainer's Dockerfile; `npx playwright install chromium` once on a
// bare host).
//
// The webServer command first seeds the throwaway data dir with the same
// file `go run ./tools/testdb` builds as ./test.db for dev (so the specs
// can use the real size-capped tables), then starts the server on it.
// Playwright starts the web server *before* a globalSetup runs, so the
// seed lives in the command chain, not a globalSetup file. The seed's
// link list is redirected to <dataDir>/seed.txt, which the specs read via
// env.mjs.
import { defineConfig } from "@playwright/test";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.join(here, "..");
// The same throwaway data dir env.mjs reads the seed links from.
const dataDir = path.join(os.tmpdir(), "kiusan-e2e");

export default defineConfig({
  testDir: here,
  testMatch: "*.spec.mjs",
  timeout: 30_000,
  // The suite is small and every test shares the one server.
  workers: 1,
  use: {
    baseURL: "http://127.0.0.1:8080",
  },
  webServer: {
    // Seed, then start: `go run` compiles twice, so a cold CI build gets a
    // generous timeout.
    command:
      `mkdir -p ${dataDir} && go run ./tools/testdb -o ${dataDir}/test.db ` +
      `-base http://127.0.0.1:8080 > ${dataDir}/seed.txt && go run .`,
    cwd: repoRoot,
    url: "http://127.0.0.1:8080/",
    // Never reuse a server already on :8080: the suite seeds and mutates
    // the throwaway database, and a reused server (e.g. a developer's)
    // would leave it on the wrong data file.
    reuseExistingServer: false,
    timeout: 300_000,
    env: {
      // A throwaway data dir per machine: the suite never reads or writes
      // the developer's meetings.db, and the config file the app persists
      // lands in the same temp dir.
      KIUSAN_DATA_DIR: dataDir,
      KIUSAN_PORT: "8080",
      KIUSAN_SECRET: "e2e-only-secret",
      // The seed file name in the data dir (the sqlite file the app reads).
      KIUSAN_DB_NAME: "test.db",
    },
  },
});
