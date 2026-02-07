package config

import (
	"fmt"
	"net"
	"strings"

	"github.com/xackery/talkeq/tlog"
)

// Web represents config settings for the web dashboard
type Web struct {
	IsEnabled bool   `toml:"enabled" desc:"Enable Web Dashboard"`
	Host      string `toml:"host" desc:"Address and port to bind the web dashboard to (default: 127.0.0.1:8080)"`
	Username  string `toml:"username" desc:"Optional HTTP Basic Auth username (leave empty to disable auth)"`
	Password  string `toml:"password" desc:"Optional HTTP Basic Auth password"`
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

	// Enforce localhost-only binding
	host, _, err := net.SplitHostPort(c.Host)
	if err != nil {
		return fmt.Errorf("[web] invalid host format %q: %w", c.Host, err)
	}

	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		tlog.Debugf("[web] host %q is not localhost-only, forcing 127.0.0.1", host)
		_, port, _ := net.SplitHostPort(c.Host)
		c.Host = "127.0.0.1:" + port
	}

	ip := net.ParseIP(host)
	if ip != nil && !ip.IsLoopback() {
		tlog.Debugf("[web] host %q is not a loopback address, forcing 127.0.0.1", host)
		_, port, _ := net.SplitHostPort(c.Host)
		c.Host = "127.0.0.1:" + port
	}

	if c.Username != "" && c.Password == "" {
		return fmt.Errorf("[web] username is set but password is empty")
	}

	return nil
}
