package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/subjut/kiusan/store"
)

// configFileName is the config file's name inside the data dir. The app
// creates it on first run (carrying the generated secret) and never
// rewrites it afterwards, so a server admin's hand edits survive restarts.
const configFileName = "kiusan.yaml"

// cfg is the loaded runtime configuration; main fills it once at startup.
var cfg Config

// Config is the runtime configuration, resolved by loadConfig. Every key
// follows env > config file > built-in default: the environment
// (KIUSAN_*) always wins, the config file (kiusan.yaml in the data dir)
// is next, and the built-in default fills whatever is still unset.
type Config struct {
	// Server.
	Port          int    // listen port (KIUSAN_PORT / port)
	BaseURL       string // public origin (KIUSAN_BASE_URL / base_url); empty falls back to the request's scheme+host
	SecureCookies bool   // derived: true while BaseURL is https
	DevMode       bool   // template reload + parse logging (KIUSAN_DEV / dev)
	Secret        string // CSRF signing secret (KIUSAN_SECRET / secret); auto-generated and persisted while no source sets it

	// Storage (KIUSAN_STORE / KIUSAN_DB_* / KIUSAN_DATA_DIR).
	Store      string // sqlite | mysql | mariadb | postgres
	DataDir    string // data dir (KIUSAN_DATA_DIR only); config file + sqlite file live here
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string // relational: database name; sqlite: the file name in DataDir
	DBSSLMode  string // postgres only
}

// configFile mirrors the keys kiusan.yaml may carry. Every key is
// optional (omitempty keeps the file minimal); an absent key simply
// contributes no value. Dev is a pointer so an explicit env "off" can be
// told apart from "absent" and persisted as `dev: false`.
type configFile struct {
	Secret  string `yaml:"secret,omitempty"`
	Port    int    `yaml:"port,omitempty"`
	BaseURL string `yaml:"base_url,omitempty"`
	Dev     *bool  `yaml:"dev,omitempty"`
	Store   string `yaml:"store,omitempty"`
	DB      struct {
		Host     string `yaml:"host,omitempty"`
		Port     string `yaml:"port,omitempty"`
		User     string `yaml:"user,omitempty"`
		Password string `yaml:"password,omitempty"`
		Name     string `yaml:"name,omitempty"`
		SSLMode  string `yaml:"sslmode,omitempty"`
	} `yaml:"db,omitempty"`
}

// loadConfig resolves the runtime configuration in three layers: built-in
// defaults, then the config file (if present), then the environment (which
// wins). Every value the env provides is also merged over the file and the
// result written back, so the file mirrors the last effective values and a
// later start without the env vars keeps them (removing an env var does not
// undo the setting). While neither the env nor the file provides a secret,
// one is generated and persisted like the rest. Returns an error for an
// unparseable file or an out-of-range port.
func loadConfig() (Config, error) {
	var c Config

	// The data dir is env-only: a file cannot carry its own location. Log the
	// resolved absolute path on every start so a misconfigured start is
	// obvious in the journal.
	c.DataDir = envOr("KIUSAN_DATA_DIR", ".")
	if abs, err := filepath.Abs(c.DataDir); err == nil {
		log.Printf("using data dir %s", abs)
	}
	path := filepath.Join(c.DataDir, configFileName)

	// Layer 1: the config file, if present. It is loaded both into c (the
	// effective config) and kept as `file` so the env's values can be
	// merged over it and the result written back.
	var file configFile
	if b, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(b, &file); err != nil {
			return Config{}, fmt.Errorf("parsing %s: %v", path, err)
		}
		c.Secret = file.Secret
		c.Port = file.Port
		c.BaseURL = file.BaseURL
		if file.Dev != nil {
			c.DevMode = *file.Dev
		}
		c.Store = file.Store
		c.DBHost = file.DB.Host
		c.DBPort = file.DB.Port
		c.DBUser = file.DB.User
		c.DBPassword = file.DB.Password
		c.DBName = file.DB.Name
		c.DBSSLMode = file.DB.SSLMode
	}

	// Layer 2: the environment, which wins over the file. Each value the
	// env provides is also merged into `file` so it is persisted.
	if v := os.Getenv("KIUSAN_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("KIUSAN_PORT must be a number, got %q", v)
		}
		c.Port = p
		file.Port = p
	}
	if v := os.Getenv("KIUSAN_BASE_URL"); v != "" {
		c.BaseURL = v
		file.BaseURL = v
	}
	if v := os.Getenv("KIUSAN_DEV"); v != "" {
		dev := true
		switch strings.ToLower(v) {
		case "0", "false", "no", "off":
			dev = false
		}
		c.DevMode = dev
		file.Dev = &dev
	}
	if v := os.Getenv("KIUSAN_SECRET"); v != "" {
		c.Secret = v
		file.Secret = v
	}
	if v := os.Getenv("KIUSAN_STORE"); v != "" {
		c.Store = v
		file.Store = v
	}
	if v := os.Getenv("KIUSAN_DB_HOST"); v != "" {
		c.DBHost = v
		file.DB.Host = v
	}
	if v := os.Getenv("KIUSAN_DB_PORT"); v != "" {
		c.DBPort = v
		file.DB.Port = v
	}
	if v := os.Getenv("KIUSAN_DB_USER"); v != "" {
		c.DBUser = v
		file.DB.User = v
	}
	if v := os.Getenv("KIUSAN_DB_PASSWORD"); v != "" {
		c.DBPassword = v
		file.DB.Password = v
	}
	if v := os.Getenv("KIUSAN_DB_NAME"); v != "" {
		c.DBName = v
		file.DB.Name = v
	}
	if v := os.Getenv("KIUSAN_DB_SSLMODE"); v != "" {
		c.DBSSLMode = v
		file.DB.SSLMode = v
	}

	// Layer 3: built-in defaults for whatever is still unset (c only — the
	// file keeps just what the env/file provided, not the derived
	// defaults).
	if c.Port == 0 {
		c.Port = 8080
	}
	if c.Store == "" {
		c.Store = store.BackendSQLite
	}
	if c.DBHost == "" {
		c.DBHost = "127.0.0.1"
	}
	if c.DBPort == "" {
		if c.Store == store.BackendPostgres {
			c.DBPort = "5432"
		} else {
			c.DBPort = "3306"
		}
	}
	if c.DBName == "" {
		c.DBName = "meetings.db"
	}
	if c.DBSSLMode == "" {
		c.DBSSLMode = "disable"
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	c.SecureCookies = strings.HasPrefix(c.BaseURL, "https://")

	if c.Port < 1 || c.Port > 65535 {
		return Config{}, fmt.Errorf("listen port must be 1-65535, got %d", c.Port)
	}

	// The secret: generated while no source provides one, then persisted
	// with the rest of the effective values.
	if c.Secret == "" {
		c.Secret = generateSecret()
		file.Secret = c.Secret
	}

	// Persist the effective values: the file's own values merged with the
	// env's (which overwrite them), so a later start without the env vars
	// keeps them.
	created, err := writeConfig(path, file)
	if err != nil {
		return Config{}, fmt.Errorf("writing %s: %v", path, err)
	}
	if created {
		log.Printf("wrote config file to %s", path)
	}

	return c, nil
}

// writeConfig writes the effective config to the file (creating the data
// dir while absent). It merges over the file's existing values rather than
// clobbering them, so a server admin's hand edits to keys the env does not
// provide survive restarts. 0600: the secret is plaintext, the file
// permission is the protection. It reports whether it created the file.
func writeConfig(path string, file configFile) (bool, error) {
	_, statErr := os.Stat(path)
	created := statErr != nil
	if created {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return false, err
		}
	}
	b, err := yaml.Marshal(file)
	if err != nil {
		return created, err
	}
	return created, os.WriteFile(path, b, 0o600)
}

// generateSecret returns a fresh secret for CSRF signing: 32 random bytes,
// hex-encoded.
func generateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Printf("warning: could not generate a secret: %v", err)
		return ""
	}
	return hex.EncodeToString(b)
}

// envOr returns the env var's value, or def while it is unset/empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// dsn builds the backend's DSN from the structured parts. KIUSAN_DSN no
// longer exists — the parts are the interface. sqlite is the data-dir path
// plus the file name; the relational backends assemble their driver DSN.
func (c Config) dsn() (string, error) {
	switch c.Store {
	case store.BackendSQLite:
		return filepath.Join(c.DataDir, c.DBName), nil
	case store.BackendMySQL, store.BackendMariaDB:
		if c.DBUser == "" {
			return "", fmt.Errorf("a database user is required for the %s backend (set KIUSAN_DB_USER)", c.Store)
		}
		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName), nil
	case store.BackendPostgres:
		if c.DBUser == "" {
			return "", fmt.Errorf("a database user is required for the postgres backend (set KIUSAN_DB_USER)")
		}
		u := url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(c.DBUser, c.DBPassword),
			Host:   c.DBHost + ":" + c.DBPort,
			Path:   "/" + c.DBName,
		}
		u.RawQuery = "sslmode=" + c.DBSSLMode
		return u.String(), nil
	default:
		return "", fmt.Errorf("unknown backend %q (valid: %s, %s, %s, %s)",
			c.Store, store.BackendMySQL, store.BackendMariaDB, store.BackendPostgres, store.BackendSQLite)
	}
}
