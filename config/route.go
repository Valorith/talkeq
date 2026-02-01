package config

import (
	"fmt"
	"regexp"
	"text/template"
)

// Route is how to route telnet messages
type Route struct {
	IsEnabled              bool    `toml:"enabled" desc:"Is route enabled?"`
	Trigger                Trigger `toml:"trigger" desc:"condition to trigger route"`
	Target                 string  `toml:"target" desc:"target service, e.g. telnet"`
	ChannelID              string  `toml:"channel_id" desc:"Destination channel ID"`
	GuildID                string  `toml:"guild_id,omitempty" desc:"Optional, Destination guild ID"`
	MessagePattern         string  `toml:"message_pattern" desc:"Destination message in. E.g. {{.Name}} says {{.ChannelName}}, '{{.Message}}"`
	messagePatternTemplate *template.Template
	triggerRegex           *regexp.Regexp
}

// MessagePatternTemplate returns a template for provided route
func (r *Route) MessagePatternTemplate() *template.Template {
	if r.messagePatternTemplate == nil {
		// fallback logic
		r.messagePatternTemplate, _ = template.New("root").Parse(r.MessagePattern)
	}
	return r.messagePatternTemplate
}

// TriggerRegex returns the pre-compiled trigger regex, or nil if invalid/empty
func (r *Route) TriggerRegex() *regexp.Regexp {
	return r.triggerRegex
}

// LoadMessagePattern is called after config is loaded, and verified patterns are valid
func (r *Route) LoadMessagePattern() error {
	if !r.IsEnabled {
		return nil
	}
	var err error
	r.messagePatternTemplate, err = template.New("root").Parse(r.MessagePattern)
	if err != nil {
		return fmt.Errorf("failed to parse message pattern: %w", err)
	}

	// Pre-compile the trigger regex if set
	if r.Trigger.Regex != "" && r.Trigger.Custom == "" {
		r.triggerRegex, err = regexp.Compile(r.Trigger.Regex)
		if err != nil {
			return fmt.Errorf("failed to compile trigger regex: %w", err)
		}
	}
	return nil
}
