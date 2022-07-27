package config

import (
	"encoding/json"
	"os"
)

type MixedRes struct {
	Namespace string `json:"namespace",omitempty`
	Name      string `json:"name",omitempty`
	Mixed     bool   `json:"mixed,omitempty"`
	Priority  int    `json:"priority`
}

type Config struct {
	Mixedreslist []*MixedRes
}

// type MixdList []*Config

func LoadFile(filename string) (*Config, error) {
	var cfg Config
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(content, &cfg.Mixedreslist)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
