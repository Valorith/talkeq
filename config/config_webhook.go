package config

import (
	"fmt"
	"strings"

	"github.com/xackery/talkeq/tlog"
)

// Webhook represents a webhook listening service for external services to post into EQ channels
type Webhook struct {
	IsEnabled bool   `toml:"enabled" desc:"Enable Webhook service\n# Allows external services to POST messages into EQ channels via HTTP"`
	Host      string `toml:"host" desc:"What address and port to bind to (default: 127.0.0.1:9934)"`
	Token     string `toml:"token" desc:"Optional Bearer token for authentication\n# If set, requests must include Authorization: Bearer <token> header\n# If empty, no authentication is required"`
}

// Verify checks if webhook config looks valid
func (c *Webhook) Verify() error {
	if !c.IsEnabled {
		return nil
	}

	if c.Host == "" {
		tlog.Debugf("[webhook] host was empty, defaulting to 127.0.0.1:9934")
		c.Host = "127.0.0.1:9934"
	}

	// Enforce localhost-only binding for security
	host := strings.Split(c.Host, ":")[0]
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return fmt.Errorf("[webhook] host must bind to localhost (127.0.0.1, localhost, or ::1), got %q — exposing the webhook externally is a security risk", host)
	}

	return nil
}
