package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Config struct {
	Host  string `json:"host,omitempty"`
	Model string `json:"model,omitempty"`
}

func Path(home string) (string, error) {
	if home == "" {
		home = os.Getenv("SELPHY_HOME")
	}
	if home == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(base, "selphy-cli")
	}
	return filepath.Join(home, "config.json"), nil
}
func Load(home string) (Config, error) {
	path, err := Path(home)
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	err = json.Unmarshal(b, &cfg)
	return cfg, err
}
func Save(home string, cfg Config) error {
	path, err := Path(home)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
