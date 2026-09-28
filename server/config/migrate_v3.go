package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
	"golang.org/x/crypto/bcrypt"
)

// MigrateV3ConfigFile converts a v3 flat-format config file to the v4 nested
// format. It returns the backup path when a migration was performed.
func MigrateV3ConfigFile(filename string) (string, error) {
	contents, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read config file: %w", err)
	}

	var values map[string]any
	if err := yaml.Unmarshal(contents, &values); err != nil {
		// Leave malformed files to the existing config loader to report.
		return "", nil
	}
	if !isV3Config(values) {
		return "", nil
	}

	var legacy v3Config
	if err := yaml.Unmarshal(contents, &legacy); err != nil {
		return "", fmt.Errorf("parse v3 config: %w", err)
	}

	converted, err := convertV3Config(legacy)
	if err != nil {
		return "", err
	}

	backupPath := v3BackupPath(filename)
	if _, err := os.Lstat(backupPath); err == nil {
		return "", fmt.Errorf("cannot migrate v3 config: backup already exists at %q", backupPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("check config backup path: %w", err)
	}

	dir := filepath.Dir(filename)
	temp, err := os.CreateTemp(dir, ".config-v4-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create converted config: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if _, err := temp.Write(converted); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("write converted config: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", fmt.Errorf("sync converted config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close converted config: %w", err)
	}

	if err := os.Rename(filename, backupPath); err != nil {
		return "", fmt.Errorf("back up v3 config: %w", err)
	}
	if err := os.Chmod(backupPath, 0600); err != nil {
		restoreErr := os.Rename(backupPath, filename)
		return "", errors.Join(fmt.Errorf("protect v3 config backup: %w", err), restoreErr)
	}
	if err := os.Rename(tempPath, filename); err != nil {
		restoreErr := os.Rename(backupPath, filename)
		return "", errors.Join(fmt.Errorf("install converted config: %w", err), restoreErr)
	}

	return backupPath, nil
}

type v3Config struct {
	LogPath            *string `yaml:"log_path"`
	EnableFileLogging  *bool   `yaml:"enable_file_logging"`
	BaseURL            *string `yaml:"base_url"`
	Host               *string `yaml:"host"`
	Port               *int    `yaml:"port"`
	DownloadPath       *string `yaml:"downloadPath"`
	DownloaderPath     *string `yaml:"downloaderPath"`
	RequireAuth        *bool   `yaml:"require_auth"`
	Username           *string `yaml:"username"`
	Password           *string `yaml:"password"`
	QueueSize          *int    `yaml:"queue_size"`
	LocalDatabasePath  *string `yaml:"local_database_path"`
	SessionFilePath    *string `yaml:"session_file_path"`
	UseOpenID          *bool   `yaml:"use_openid"`
	OpenIDProviderURL  *string `yaml:"openid_provider_url"`
	OpenIDClientID     *string `yaml:"openid_client_id"`
	OpenIDClientSecret *string `yaml:"openid_client_secret"`
	OpenIDRedirectURL  *string `yaml:"openid_redirect_url"`
	FrontendPath       *string `yaml:"frontend_path"`
	AutoArchive        *bool   `yaml:"auto_archive"`
}

func isV3Config(values map[string]any) bool {
	if len(values) == 0 {
		return false
	}
	for key := range values {
		switch strings.ToLower(key) {
		case "server", "logging", "paths", "authentication", "openid", "frontend", "twitch":
			// A v4 section means the file already uses the v4 structure.
			return false
		}
	}

	for key := range values {
		switch strings.ToLower(key) {
		case "base_url", "host", "port", "log_path", "enable_file_logging",
			"downloadpath", "downloaderpath", "require_auth", "username", "password",
			"queue_size", "local_database_path", "session_file_path", "use_openid",
			"openid_provider_url", "openid_client_id", "openid_client_secret",
			"openid_redirect_url", "frontend_path":
			return true
		}
	}
	return false
}

func convertV3Config(legacy v3Config) ([]byte, error) {
	converted := make(map[string]any)
	server := make(map[string]any)
	set(server, "base_url", legacy.BaseURL)
	set(server, "host", legacy.Host)
	set(server, "port", legacy.Port)
	set(server, "queue_size", legacy.QueueSize)
	if len(server) > 0 {
		converted["server"] = server
	}

	logging := make(map[string]any)
	set(logging, "log_path", legacy.LogPath)
	set(logging, "enable_file_logging", legacy.EnableFileLogging)
	if len(logging) > 0 {
		converted["logging"] = logging
	}

	paths := make(map[string]any)
	set(paths, "download_path", legacy.DownloadPath)
	set(paths, "downloader_path", legacy.DownloaderPath)
	if legacy.LocalDatabasePath != nil {
		// v3 stored a SQLite file path; v4 stores a directory containing bolt.db.
		databaseDir := filepath.Dir(*legacy.LocalDatabasePath)
		if databaseDir == "" {
			databaseDir = "."
		}
		paths["local_database_path"] = databaseDir
		slog.Warn("v3 SQLite data is not migrated; using its parent directory for the v4 database", "legacy_path", *legacy.LocalDatabasePath, "v4_directory", databaseDir)
	}
	if len(paths) > 0 {
		converted["paths"] = paths
	}
	frontend := make(map[string]any)
	set(frontend, "frontend_path", legacy.FrontendPath)
	if len(frontend) > 0 {
		converted["frontend"] = frontend
	}

	authentication := make(map[string]any)
	set(authentication, "require_auth", legacy.RequireAuth)
	set(authentication, "username", legacy.Username)
	if legacy.Password != nil {
		passwordHash, err := bcrypt.GenerateFromPassword([]byte(*legacy.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("hash v3 password for v4 config: %w", err)
		}
		authentication["password_hash"] = string(passwordHash)
	}
	if len(authentication) > 0 {
		converted["authentication"] = authentication
	}

	openid := make(map[string]any)
	set(openid, "use_openid", legacy.UseOpenID)
	set(openid, "provider_url", legacy.OpenIDProviderURL)
	set(openid, "client_id", legacy.OpenIDClientID)
	set(openid, "client_secret", legacy.OpenIDClientSecret)
	set(openid, "redirect_url", legacy.OpenIDRedirectURL)
	if len(openid) > 0 {
		converted["openid"] = openid
	}

	set(converted, "auto_archive", legacy.AutoArchive)
	if legacy.SessionFilePath != nil && *legacy.SessionFilePath != "" {
		slog.Warn("v3 session files are not migrated to v4", "legacy_path", *legacy.SessionFilePath)
	}

	return yaml.Marshal(converted)
}

func set(section map[string]any, key string, value any) {
	switch typed := value.(type) {
	case *string:
		if typed != nil {
			section[key] = *typed
		}
	case *bool:
		if typed != nil {
			section[key] = *typed
		}
	case *int:
		if typed != nil {
			section[key] = *typed
		}
	}
}

func v3BackupPath(filename string) string {
	ext := filepath.Ext(filename)
	return strings.TrimSuffix(filename, ext) + ".v3" + ext
}
