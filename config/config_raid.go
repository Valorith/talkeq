package config

import "fmt"

// Raid represents raid attendance integration configuration
type Raid struct {
	IsEnabled           bool   `toml:"enabled" desc:"Enable raid attendance integration with CW Raid Manager"`
	APIURL              string `toml:"api_url" desc:"CW Raid Manager API base URL (e.g. https://raids.example.com)"`
	APIToken            string `toml:"api_token" desc:"Authentication token (JWT) for CW Raid Manager API"`
	RaidEventID         string `toml:"raid_event_id" desc:"Current raid event ID to post attendance against"`
	DiscordChannelID    string `toml:"discord_channel_id" desc:"Discord channel ID for raid attendance notifications"`
	TriggerCommand      string `toml:"trigger_command" desc:"Telnet regex pattern to trigger a raid dump (default: raid dump trigger from telnet)"`
	TelnetDumpCommand   string `toml:"telnet_dump_command" desc:"Command sent to telnet to request raid dump (e.g. #raidlist)"`
	DumpFilePath        string `toml:"dump_file_path" desc:"Path to raid dump file if using file-based dumps (optional)"`
	AutoPost            bool   `toml:"auto_post" desc:"Automatically POST attendance when raid dump is detected"`
	NotifyDiscord       bool   `toml:"notify_discord" desc:"Send Discord embed notification when attendance is synced"`
}

// Verify checks raid configuration
func (r *Raid) Verify() error {
	if !r.IsEnabled {
		return nil
	}
	if r.APIURL == "" {
		return fmt.Errorf("raid api_url must be set when raid is enabled")
	}
	if r.APIToken == "" {
		return fmt.Errorf("raid api_token must be set when raid is enabled")
	}
	if r.RaidEventID == "" {
		return fmt.Errorf("raid raid_event_id must be set when raid is enabled")
	}
	return nil
}
