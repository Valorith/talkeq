package config

import "github.com/xackery/talkeq/tlog"

// Web represents config settings for the web dashboard
type Web struct {
	IsEnabled bool   `toml:"enabled" desc:"Enable Web Dashboard"`
	Host      string `toml:"host" desc:"Address and port to bind the web dashboard to"`
}

// Verify checks if web config looks valid
func (c *Web) Verify() error {
	if !c.IsEnabled {
		return nil
	}

	if c.Host == "" {
		tlog.Debugf("[web] host was empty, defaulting to 127.0.0.1:8080")
		c.Host = "127.0.0.1:8080"
	}

	return nil
}
