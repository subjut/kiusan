// The per-client create rate limit (20 / minute on the unauthenticated
// write routes): the 21st meeting creation within a window gets a 429.
//
// The spec runs its OWN server (fresh port, fresh data dir, fresh
// in-memory limiter state): the main suite's server is shared by every
// spec under one client IP, and probing the real budget there would
// exhaust it for the journey specs' own creates in the same minute.
import { test, expect } from "@playwright/test";
import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const base = "http://127.0.0.1:8081";

// Teardown even on a failed test: kill the whole process group (go *and*
// the server binary it spawned), or the server is orphaned on :8081.
let srv = null;
test.afterAll(() => {
	if (srv) {
		try {
			process.kill(-srv.pid, "SIGKILL");
		} catch { /* already gone */ }
		srv = null;
	}
});

test("the 21st meeting creation within a minute gets 429", async ({ request }) => {
	// A throwaway data dir of its own (the suite's data dir is the
	// main server's; the app also persists its config file next to it).
	const dataDir = path.join(os.tmpdir(), "kiusan-e2e-ratelimit");
	fs.rmSync(dataDir, { recursive: true, force: true });
	fs.mkdirSync(dataDir, { recursive: true });

	// detached: the go process leads its own group, so teardown can kill
	// go *and* the server binary it spawns (a SIGKILL to go run alone
	// would orphan the server on :8081).
	const serverLog = path.join(dataDir, "server.log");
	const logFd = fs.openSync(serverLog, "a");
	srv = spawn("go", ["run", "."], {
		cwd: repoRoot,
		env: {
			...process.env,
			KIUSAN_DATA_DIR: dataDir,
			KIUSAN_PORT: "8081",
			KIUSAN_SECRET: "e2e-ratelimit-secret",
		},
		detached: true,
		stdio: ["ignore", logFd, logFd],
	});
	fs.closeSync(logFd);

	// Cold `go run` (e.g. on a fresh CI runner) can take a while.
	test.setTimeout(180_000);
	for (let i = 0; i < 240; i++) {
		try {
			const r = await fetch(base + "/");
			if (r.ok) break;
		} catch {
			/* not up yet */
		}
		await new Promise((r) => setTimeout(r, 500));
	}

	const statuses = [];
	for (let i = 1; i <= 21; i++) {
		const res = await request.post(base + "/meet-form", {
			form: { title: `Rate limit probe ${i}`, slot0: "2027-01-01" },
		});
		statuses.push(res.status());
	}
	// The first 20 create (the redirect is followed to the admin page).
	// On failure, attach the server's log tail: the 500s' own bodies are
	// generic, the real error is only in the server's stdout.
	const logTail = () =>
		fs.readFileSync(serverLog, "utf8").split("\n").slice(-25).join("\n");
	for (let i = 0; i < 20; i++) {
		expect(statuses[i], `create ${i + 1} failed; server log:\n${logTail()}`)
			.toBe(200);
	}
	// The 21st is rejected by the limiter.
	expect(statuses[20]).toBe(429);
});
