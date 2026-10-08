package config

import "fmt"

type MultiUserConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	DataDir         string `yaml:"data-dir" json:"data-dir"`
	SessionTTLHours int    `yaml:"session-ttl-hours" json:"session-ttl-hours"`
}

func (c MultiUserConfig) Validate() error {
	if c.SessionTTLHours < 0 || c.SessionTTLHours > 8760 {
		return fmt.Errorf("multi-user.session-ttl-hours must be between 0 and 8760")
	}
	return nil
}
