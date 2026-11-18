package main

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionRaw string

// Version is the release version of the running build, compiled in from the
// VERSION file at the repo root — the one place a version bump happens.
var Version = strings.TrimSpace(versionRaw)
