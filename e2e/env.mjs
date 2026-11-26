// Shared by the e2e specs: the throwaway data dir (the same path
// playwright.config.mjs gives the seeded server) and the parsed links the
// webServer's `go run ./tools/testdb` step wrote into <dataDir>/seed.txt.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

export const dataDir = path.join(os.tmpdir(), "kiusan-e2e");

// seedTables: the link blocks the seed prints — per table its label (the
// generator's table name without the "seed " prefix) and the full `admin` /
// `user` URLs (the seed is told to print them with the suite's origin).
export function seedTables() {
	const text = fs.readFileSync(path.join(dataDir, "seed.txt"), "utf8");
	const tables = [];
	let cur = null;
	for (const line of text.split("\n")) {
		const label = line.match(/^seed (.+)$/);
		if (label) {
			cur = { label: label[1], admin: null, user: null };
			tables.push(cur);
			continue;
		}
		const link = line.match(/^\s*(admin|user):\s*(\S+)/);
		if (link && cur) cur[link[1]] = link[2];
	}
	return tables;
}

// seed: the seed table with the exact label the generator prints.
export function seed(label) {
	const t = seedTables().find((x) => x.label === label);
	if (!t) throw new Error("seed table not found: " + label);
	return t;
}
