package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearKiusanEnv blanks every KIUSAN_* var the config layer reads, so the
// tests are hermetic regardless of the developer's environment
// (t.Setenv restores each prior value after the test).
func clearKiusanEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"KIUSAN_DATA_DIR", "KIUSAN_PORT", "KIUSAN_BASE_URL", "KIUSAN_DEV",
		"KIUSAN_SECRET", "KIUSAN_STORE", "KIUSAN_DB_HOST", "KIUSAN_DB_PORT",
		"KIUSAN_DB_USER", "KIUSAN_DB_PASSWORD", "KIUSAN_DB_NAME", "KIUSAN_DB_SSLMODE",
	} {
		t.Setenv(k, "")
	}
}

// TestConfigDefaults pins the default resolution in an empty data dir
// (no file, no env): the built-in defaults, a generated secret, and the
// config file created at 0600 carrying it.
func TestConfigDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	clearKiusanEnv(t)

	c, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.Port != 8080 || c.Store != "sqlite" || c.DataDir != "." ||
		c.DBHost != "127.0.0.1" || c.DBPort != "3306" ||
		c.DBName != "meetings.db" || c.DBSSLMode != "disable" ||
		c.DevMode || c.BaseURL != "" || c.SecureCookies {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.Secret == "" {
		t.Fatal("expected a generated secret")
	}

	// The config file exists, is 0600, and carries the secret.
	fi, err := os.Stat("kiusan.yaml")
	if err != nil {
		t.Fatalf("config file missing: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode: %v, want 0600", fi.Mode().Perm())
	}
	b, _ := os.ReadFile("kiusan.yaml")
	if !strings.Contains(string(b), c.Secret) {
		t.Fatalf("config file does not carry the generated secret")
	}

	// A second load resolves the same secret from the file (the rewritten
	// file's content is stable across restarts while the env is unchanged).
	c2, err := loadConfig()
	if err != nil {
		t.Fatalf("second loadConfig: %v", err)
	}
	if c2.Secret != c.Secret {
		t.Fatalf("secret not stable across loads: %q vs %q", c.Secret, c2.Secret)
	}
}

// TestConfigPrecedence pins the env > file > default resolution per key.
func TestConfigPrecedence(t *testing.T) {
	t.Chdir(t.TempDir())
	clearKiusanEnv(t)

	file := "secret: file-secret\n" +
		"port: 9090\n" +
		"base_url: \"https://file.example/\"\n" +
		"dev: true\n" +
		"store: mysql\n" +
		"db:\n" +
		"  host: filehost\n" +
		"  port: \"3307\"\n" +
		"  user: fileuser\n" +
		"  password: filepass\n" +
		"  name: filedb\n" +
		"  sslmode: require\n"
	if err := os.WriteFile("kiusan.yaml", []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	// The env overrides port and base_url; the rest stay file-sourced.
	t.Setenv("KIUSAN_PORT", "9999")
	t.Setenv("KIUSAN_BASE_URL", "https://env.example/")

	c, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.Port != 9999 {
		t.Fatalf("Port: %d, want 9999 (env over file)", c.Port)
	}
	if c.BaseURL != "https://env.example" {
		t.Fatalf("BaseURL: %q, want the env value trimmed", c.BaseURL)
	}
	if !c.SecureCookies {
		t.Fatal("SecureCookies: want true for an https base URL")
	}
	if c.Secret != "file-secret" {
		t.Fatalf("Secret: %q, want the file value", c.Secret)
	}
	if !c.DevMode || c.Store != "mysql" {
		t.Fatalf("DevMode/Store: %v %q, want the file values", c.DevMode, c.Store)
	}
	if c.DBHost != "filehost" || c.DBPort != "3307" || c.DBUser != "fileuser" ||
		c.DBPassword != "filepass" || c.DBName != "filedb" || c.DBSSLMode != "require" {
		t.Fatalf("db parts: %+v, want the file values", c)
	}

	// The file was rewritten with the effective values: the env's overwrote
	// the file's, the file's other values (no env for them) survived.
	b, err := os.ReadFile("kiusan.yaml")
	if err != nil {
		t.Fatalf("reading the rewritten config file: %v", err)
	}
	fb := string(b)
	for _, want := range []string{
		"port: 9999",
		"env.example/",
		"secret: file-secret",
		"store: mysql",
		"user: fileuser",
		"sslmode: require",
	} {
		if !strings.Contains(fb, want) {
			t.Fatalf("config file after load missing %q:\n%s", want, fb)
		}
	}
	if strings.Contains(fb, "9090") {
		t.Fatalf("config file still carries the overwritten port:\n%s", fb)
	}
	if strings.Contains(fb, "file.example") {
		t.Fatalf("config file still carries the overwritten base_url:\n%s", fb)
	}

	// A later start without the env vars keeps the values (they were
	// persisted): file > defaults.
	t.Setenv("KIUSAN_PORT", "")
	t.Setenv("KIUSAN_BASE_URL", "")
	c3, err := loadConfig()
	if err != nil {
		t.Fatalf("third loadConfig: %v", err)
	}
	if c3.Port != 9999 || c3.BaseURL != "https://env.example" ||
		c3.Store != "mysql" || c3.Secret != "file-secret" || !c3.DevMode {
		t.Fatalf("values not kept without the env vars: %+v", c3)
	}
}

// TestConfigEnvSecret pins that an env-provided secret is used as-is and
// persisted to the config file (the env is the setter-of-record).
func TestConfigEnvSecret(t *testing.T) {
	t.Chdir(t.TempDir())
	clearKiusanEnv(t)
	t.Setenv("KIUSAN_SECRET", "env-secret")

	c, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.Secret != "env-secret" {
		t.Fatalf("Secret: %q", c.Secret)
	}
	b, err := os.ReadFile("kiusan.yaml")
	if err != nil {
		t.Fatalf("config file missing: %v", err)
	}
	if !strings.Contains(string(b), "env-secret") {
		t.Fatalf("config file does not carry the env-provided secret")
	}
}

// TestConfigDevOffPersists pins that an explicit env dev "off" is
// persisted as `dev: false` (not dropped like an absent key).
func TestConfigDevOffPersists(t *testing.T) {
	t.Chdir(t.TempDir())
	clearKiusanEnv(t)
	t.Setenv("KIUSAN_DEV", "0")

	c, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.DevMode {
		t.Fatal("DevMode: want false for KIUSAN_DEV=0")
	}
	b, err := os.ReadFile("kiusan.yaml")
	if err != nil {
		t.Fatalf("config file missing: %v", err)
	}
	if !strings.Contains(string(b), "dev: false") {
		t.Fatalf("config file missing `dev: false`:\n%s", b)
	}
}

// TestConfigFileWithoutSecretGainsOne pins that a config file without a
// secret (hand-made, or env-secret removed later) gains the generated one
// on load — it is persisted like every other effective value.
func TestConfigFileWithoutSecretGainsOne(t *testing.T) {
	t.Chdir(t.TempDir())
	clearKiusanEnv(t)
	if err := os.WriteFile("kiusan.yaml", []byte("store: sqlite\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.Secret == "" {
		t.Fatal("expected a generated secret")
	}
	b, err := os.ReadFile("kiusan.yaml")
	if err != nil {
		t.Fatalf("reading the rewritten config file: %v", err)
	}
	if !strings.Contains(string(b), c.Secret) {
		t.Fatalf("config file does not carry the generated secret")
	}
}

// TestConfigDataDir pins that KIUSAN_DATA_DIR (env-only) locates the
// config file and the sqlite file, creating the directory while absent.
func TestConfigDataDir(t *testing.T) {
	t.Chdir(t.TempDir())
	clearKiusanEnv(t)
	t.Setenv("KIUSAN_DATA_DIR", "nested/data")

	c, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.DataDir != "nested/data" {
		t.Fatalf("DataDir: %q", c.DataDir)
	}
	if _, err := os.Stat("nested/data/kiusan.yaml"); err != nil {
		t.Fatalf("config file missing in the data dir: %v", err)
	}
	dsn, err := c.dsn()
	if err != nil {
		t.Fatalf("dsn(): %v", err)
	}
	if dsn != filepath.Join("nested", "data", "meetings.db") {
		t.Fatalf("dsn: %q", dsn)
	}
}

// TestConfigErrors pins the fail-fast contract: a non-numeric or
// out-of-range port and a malformed config file are startup errors with
// an actionable message.
func TestConfigErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	clearKiusanEnv(t)

	t.Setenv("KIUSAN_PORT", "not-a-port")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "KIUSAN_PORT") {
		t.Fatalf("non-numeric port: err = %v, want one naming KIUSAN_PORT", err)
	}
	t.Setenv("KIUSAN_PORT", "70000")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("out-of-range port: err = %v", err)
	}
	t.Setenv("KIUSAN_PORT", "")

	if err := os.WriteFile("kiusan.yaml", []byte("port: [broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("malformed config file: err = %v", err)
	}
}

// TestConfigDSN pins the DSN construction from the structured parts for
// every backend (KIUSAN_DSN no longer exists), including postgres'
// password escaping and the missing-user validation.
func TestConfigDSN(t *testing.T) {
	tests := []struct {
		name    string
		c       Config
		want    string
		wantErr string
	}{
		{"sqlite joins data dir and file name",
			Config{Store: "sqlite", DataDir: "data", DBName: "test.db"}, "data/test.db", ""},
		{"mysql",
			Config{Store: "mysql", DBUser: "u", DBPassword: "p", DBHost: "h", DBPort: "3306", DBName: "d"},
			"u:p@tcp(h:3306)/d", ""},
		{"mariadb",
			Config{Store: "mariadb", DBUser: "u", DBPassword: "p", DBHost: "h", DBPort: "3306", DBName: "d"},
			"u:p@tcp(h:3306)/d", ""},
		{"postgres",
			Config{Store: "postgres", DBUser: "u", DBPassword: "p", DBHost: "h", DBPort: "5432", DBName: "d", DBSSLMode: "disable"},
			"postgres://u:p@h:5432/d?sslmode=disable", ""},
		{"postgres escapes special password characters",
			Config{Store: "postgres", DBUser: "u", DBPassword: "p@ss:word", DBHost: "h", DBPort: "5432", DBName: "d", DBSSLMode: "require"},
			"postgres://u:p%40ss%3Aword@h:5432/d?sslmode=require", ""},
		{"mysql without a user", Config{Store: "mysql"}, "", "KIUSAN_DB_USER"},
		{"postgres without a user", Config{Store: "postgres"}, "", "KIUSAN_DB_USER"},
		{"unknown backend", Config{Store: "oracle"}, "", "unknown backend"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.c.dsn()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("dsn() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("dsn(): %v", err)
			}
			if got != tt.want {
				t.Fatalf("dsn() = %q, want %q", got, tt.want)
			}
		})
	}
}
