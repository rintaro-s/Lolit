package config

import (
	"os"
	"path/filepath"
)

// Config holds server configuration.
type Config struct {
	ListenAddr     string
	DataDir        string
	DBPath         string
	IndexPath      string
	GiteaURL       string
	GiteaAdminUser string
	GiteaAdminPass string
	JWTSecret      string
	WebhookSecret  string // if set, incoming Gitea webhooks must present a matching HMAC signature
	ReposRoot      string // path where Gitea stores bare repos (for metadata extraction)
	PreviewDir     string
}

func Load() Config {
	dataDir := getEnv("LOLIT_DATA_DIR", "/var/lib/lolit")
	return Config{
		ListenAddr:     getEnv("LOLIT_LISTEN", ":8080"),
		DataDir:        dataDir,
		DBPath:         filepath.Join(dataDir, "lolit.db"),
		IndexPath:      filepath.Join(dataDir, "bleve.idx"),
		GiteaURL:       getEnv("LOLIT_GITEA_URL", "http://localhost:3000"),
		GiteaAdminUser: getEnv("LOLIT_GITEA_USER", "gitea_admin"),
		GiteaAdminPass: getEnv("LOLIT_GITEA_PASS", ""),
		JWTSecret:      getEnv("LOLIT_JWT_SECRET", "change-me-in-production"),
		WebhookSecret:  getEnv("LOLIT_WEBHOOK_SECRET", ""),
		ReposRoot:      getEnv("LOLIT_REPOS_ROOT", "/var/lib/gitea/data/gitea-repositories"),
		PreviewDir:     filepath.Join(dataDir, "previews"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (c *Config) EnsureDirs() error {
	for _, d := range []string{c.DataDir, c.PreviewDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}
