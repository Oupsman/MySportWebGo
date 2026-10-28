// Package config holds the settings of the application and its files
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"MySportWebSync/internal/device"
)

type Config struct {
	ServerURL string `json:"server_url"` // the URL of MySportWeb, as opened in the browser
	// Token is a device token holding activities:create, created in the settings of MySportWeb
	Token    string   `json:"token"`
	Folders  []string `json:"folders"`
	AutoSync bool     `json:"auto_sync"` // synchronize a device when it is plugged
	// MaxAgeDays ignores the files older than this, 0 synchronizes all of them
	MaxAgeDays int `json:"max_age_days"`
}

func Default() Config {
	return Config{Folders: device.DefaultFolders, AutoSync: true}
}

// Dir is the directory of the files of the application
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mysportweb-sync"), nil
}

func path() (string, error) {
	dir, err := Dir()
	return filepath.Join(dir, "config.json"), err
}

// Load returns the saved settings, the default ones before the first save
func Load() (Config, error) {
	cfg := Default()
	p, err := path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	if len(cfg.Folders) == 0 {
		cfg.Folders = device.DefaultFolders
	}
	return cfg, nil
}

// Save writes the settings, readable by the user only: they hold the token
func Save(cfg Config) error {
	p, err := path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(p, data)
}

// WriteFile replaces a file of the application atomically, readable by the user only
func WriteFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // fails harmlessly once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
