package config

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/jbsmith7741/toml"
	"github.com/rs/zerolog"
)

// Config represents a configuration parse
type Config struct {
	Debug                         bool      `toml:"debug" desc:"TalkEQ Configuration\n\n# Debug messages are displayed. This will cause console to be more verbose, but also more informative"`
	IsKeepAliveEnabled            bool      `toml:"keep_alive" desc:"Keep all connections alive?\n# If false, endpoint disconnects will not self repair\n# Not recommended to turn off except in advanced cases"`
	KeepAliveRetry                string    `toml:"keep_alive_retry" desc:"How long before retrying to connect (requires keep_alive = true)\n# default: 10s"`
	IsFallbackGuildChannelEnabled bool      `toml:"is_fallback_guild_channel_enabled" desc:"If a guild chat occurs and it isn't mapped inside talkeq_guilds, chat is echod to the globalguild channel route channelid"`
	UsersDatabasePath             string    `toml:"users_database" desc:"Users by ID are mapped to their display names via the raw text file called users database\n# If users database file does not exist, a new one is created\n# This file is actively monitored. if you edit it while talkeq is running, it will reload the changes instantly\n# This file overrides the IGN: playerName role tags in discord\n# If a user is not found on this list, it will fall back to check for IGN tags"`
	GuildsDatabasePath            string    `toml:"guilds_database" desc:"Guilds by ID are mapped to their database ID via the raw text file called guilds database\n# If guilds database file does not exist, a new one is created\n# This file is actively monitored. if you edit it while talkeq is running, it will reload the changes instantly"`
	API                           API       `toml:"api" desc:"NOT YET SUPPORTED, can be ignored for now (it's fine to keep enabled): API is a service to allow external tools to talk to TalkEQ via HTTP requests.\n# It uses Restful style (JSON) with a /api suffix for all endpoints"`
	Discord                       Discord   `toml:"discord" desc:"Discord is a chat service that you can listen and relay EQ chat with"`
	Telnet                        Telnet    `toml:"telnet" desc:"Telnet is a service eqemu/server can use, that relays messages over"`
	EQLog                         EQLog     `toml:"eqlog" desc:"EQ Log is used to parse everquest client logs. Primarily for live EQ, non server owners"`
	PEQEditor                     PEQEditor `toml:"peq_editor"`
	SQLReport                     SQLReport `toml:"sql_report" desc:"SQL Report can be used to show stats on discord\n# An ideal way to set this up is create a private voice channel\n# Then bind it to various queries"`
}

// Trigger is a regex pattern matching
type Trigger struct {
	Regex        string `toml:"telnet_pattern" desc:"Input telnet trigger regex"`
	NameIndex    int    `toml:"name_index" desc:"Name is found in this regex index grouping (0 is ignored)"`
	MessageIndex int    `toml:"message_index" desc:"Message is found in this regex index grouping (0 is ignored)"`
	GuildIndex   int    `toml:"guild_index" desc:"Guild is found in this regex index grouping (0 is ignored)"`
	Custom       string `toml:"custom,omitempty" dec:"Custom event defined in code"`
}

// NewConfig creates a new configuration
func NewConfig(ctx context.Context) (*Config, error) {
	var f *os.File
	cfg := Config{}
	path := "talkeq.conf"

	isNewConfig := false
	fi, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("config info: %w", err)
		}
		f, err = os.Create(path)
		if err != nil {
			return nil, fmt.Errorf("create talkeq.conf: %w", err)
		}
		fi, err = os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("new config info: %w", err)
		}
		isNewConfig = true
	}
	if !isNewConfig {
		f, err = os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open config: %w", err)
		}
	}

	defer f.Close()
	if fi.IsDir() {
		return nil, fmt.Errorf("talkeq.conf is a directory, should be a file")
	}

	if isNewConfig {
		enc := toml.NewEncoder(f)
		enc.Encode(getDefaultConfig())

		fmt.Println("a new talkeq.conf file was created. Please open this file and configure talkeq, then run it again.")
		if runtime.GOOS == "windows" {
			option := ""
			fmt.Println("press a key then enter to exit.")
			fmt.Scan(&option)
		}
		os.Exit(0)
	}

	_, err = toml.DecodeReader(f, &cfg)
	if err != nil {
		return nil, fmt.Errorf("decode talkeq.conf: %w", err)
	}

	/*fw, err := os.Create("talkeq2.toml")
	if err != nil {
		return nil, fmt.Errorf("talkeq: %w", err)
	}
	defer fw.Close()

	enc := toml.NewEncoder(fw)
	err = enc.Encode(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}*/

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if cfg.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	sort.SliceStable(cfg.SQLReport.Entries, func(i, j int) bool {
		return cfg.SQLReport.Entries[i].Index > cfg.SQLReport.Entries[j].Index
	})

	err = cfg.Verify()
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}

	return &cfg, nil
}

// Verify returns an error if configuration appears off
func (c *Config) Verify() error {

	if c.UsersDatabasePath == "" {
		c.UsersDatabasePath = "talkeq_users.txt"
	}

	if c.GuildsDatabasePath == "" {
		c.GuildsDatabasePath = "./guilds.txt"
	}

	if c.IsKeepAliveEnabled && c.KeepAliveRetryDuration().Seconds() < 2 {
		c.KeepAliveRetry = "30s"
	}

	if err := c.API.Verify(); err != nil {
		return fmt.Errorf("api: %w", err)
	}
	if err := c.Discord.Verify(); err != nil {
		return fmt.Errorf("discord: %w", err)
	}
	if err := c.EQLog.Verify(); err != nil {
		return fmt.Errorf("eqlog: %w", err)
	}
	if err := c.PEQEditor.Verify(); err != nil {
		return fmt.Errorf("peqeditor: %w", err)
	}
	if err := c.SQLReport.Verify(); err != nil {
		return fmt.Errorf("sqlreport: %w", err)
	}
	if err := c.Telnet.Verify(); err != nil {
		return fmt.Errorf("telnet: %w", err)
	}

	// Resolve channel type names to Discord channel IDs using discord.channels map
	c.resolveChannelMappings()

	return nil
}

// resolveChannelMappings replaces channel type names (e.g. "ooc", "auction") in route
// channel_id fields with actual Discord channel IDs from the discord.channels map.
// This allows users to define channel IDs once in [discord.channels] and reference them
// by name in routes, while remaining backwards compatible with literal channel IDs.
func (c *Config) resolveChannelMappings() {
	if len(c.Discord.Channels) == 0 {
		return
	}
	for i := range c.Telnet.Routes {
		c.Telnet.Routes[i].ChannelID = c.Discord.ResolveChannelID(c.Telnet.Routes[i].ChannelID)
	}
	for i := range c.EQLog.Routes {
		c.EQLog.Routes[i].ChannelID = c.Discord.ResolveChannelID(c.EQLog.Routes[i].ChannelID)
	}
	for i := range c.PEQEditor.SQL.Routes {
		c.PEQEditor.SQL.Routes[i].ChannelID = c.Discord.ResolveChannelID(c.PEQEditor.SQL.Routes[i].ChannelID)
	}
	for i := range c.Discord.Routes {
		c.Discord.Routes[i].Trigger.ChannelID = c.Discord.ResolveChannelID(c.Discord.Routes[i].Trigger.ChannelID)
	}
}

// KeepAliveRetryDuration returns the converted retry rate
func (c *Config) KeepAliveRetryDuration() time.Duration {
	retryDuration, err := time.ParseDuration(c.KeepAliveRetry)
	if err != nil {
		return 10 * time.Second
	}

	if retryDuration < 10*time.Second {
		return 10 * time.Second
	}
	return retryDuration
}

func getDefaultConfig() Config {
	cfg := Config{
		Debug:              true,
		IsKeepAliveEnabled: true,
		KeepAliveRetry:     "10s",
		UsersDatabasePath:  "talkeq_users.txt",
		GuildsDatabasePath: "talkeq_guilds.txt",
	}
	cfg.API.IsEnabled = true
	cfg.API.Host = ":9933"
	cfg.API.APIRegister.IsEnabled = true
	cfg.API.APIRegister.RegistrationDatabasePath = "talkeq_register.toml"

	cfg.Discord.IsEnabled = true
	cfg.Discord.BotStatus = "EQ: {{.PlayerCount}} Online"
	cfg.Discord.Channels = map[string]string{
		"ooc":       "INSERTOOCCHANNELHERE",
		"auction":   "INSERTAUCTIONCHANNELHERE",
		"general":   "INSERTGENERALCHANNELHERE",
		"guild":     "INSERTGLOBALGUILDCHANNELHERE",
		"shout":     "INSERTSHOUTCHANNELHERE",
		"broadcast": "INSERTBROADCASTCHANNELHERE",
		"peqeditor": "INSERTPEQEDITORLOGCHANNELHERE",
	}
	cfg.Discord.Routes = append(cfg.Discord.Routes, DiscordRoute{
		IsEnabled: true,
		Trigger: DiscordTrigger{
			ChannelID: "ooc",
		},
		Target:         "telnet",
		ChannelID:      "260",
		MessagePattern: "emote world {{.ChannelID}} {{.Name}} says from discord, '{{.Message}}'",
	})

	cfg.Telnet.IsEnabled = true
	cfg.Telnet.Host = "127.0.0.1:9000"
	cfg.Telnet.ItemURL = "http://everquest.allakhazam.com/db/item.html?item="
	cfg.Telnet.IsServerAnnounceEnabled = true
	cfg.Telnet.IsOOCAuctionEnabled = true
	cfg.Telnet.Routes = append(cfg.Telnet.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Regex:        `(\w+) says ooc, '(.*)'`,
			NameIndex:    1,
			MessageIndex: 2,
		},
		Target:         "discord",
		ChannelID:      "ooc",
		MessagePattern: "{{.Name}} **OOC**: {{.Message}}",
	})

	cfg.Telnet.Routes = append(cfg.Telnet.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Regex:        `(\w+) auctions, '(.*)'`,
			NameIndex:    1,
			MessageIndex: 2,
		},
		Target:         "discord",
		ChannelID:      "auction",
		MessagePattern: "{{.Name}} **auction**: {{.Message}}",
	})

	cfg.Telnet.Routes = append(cfg.Telnet.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Regex:        `(\w+) general, '(.*)'`,
			NameIndex:    1,
			MessageIndex: 2,
		},
		Target:         "discord",
		ChannelID:      "general",
		MessagePattern: "{{.Name}} **general**: {{.Message}}",
	})

	cfg.Telnet.Routes = append(cfg.Telnet.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Regex:        `(\w+) BROADCASTS, '(.*)'`,
			NameIndex:    1,
			MessageIndex: 2,
		},
		Target:         "discord",
		ChannelID:      "broadcast",
		MessagePattern: "{{.Name}} **BROADCAST**: {{.Message}}",
	})

	cfg.Telnet.Routes = append(cfg.Telnet.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Custom: "serverup",
		},
		Target:         "discord",
		ChannelID:      "ooc",
		MessagePattern: "**Admin ooc:** Server is now UP",
	})
	cfg.Telnet.Routes = append(cfg.Telnet.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Custom: "serverdown",
		},
		Target:         "discord",
		ChannelID:      "ooc",
		MessagePattern: "**Admin ooc:** Server is now DOWN",
	})

	cfg.Telnet.Routes = append(cfg.Telnet.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Regex:        `(\w+) tells the guild \[([0-9]+)\], '(.*)'`,
			NameIndex:    1,
			GuildIndex:   2,
			MessageIndex: 3,
		},
		Target:         "discord",
		ChannelID:      "guild",
		MessagePattern: "{{.Name}} **GUILD**: {{.Message}}",
	})

	cfg.EQLog.Path = `c:\Program Files\Everquest\Logs\eqlog_CharacterName_Server.txt`
	cfg.EQLog.Routes = append(cfg.EQLog.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Regex:        `(\w+) says out of character, '(.*)'`,
			NameIndex:    1,
			MessageIndex: 2,
		},
		Target:         "discord",
		ChannelID:      "ooc",
		MessagePattern: "{{.Name}} **OOC**: {{.Message}}",
	})
	cfg.EQLog.Routes = append(cfg.EQLog.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Regex:        `(\w+) auctions, '(.*)'`,
			NameIndex:    1,
			MessageIndex: 2,
		},
		Target:         "discord",
		ChannelID:      "auction",
		MessagePattern: "{{.Name}} **AUCTION**: {{.Message}}",
	})
	cfg.EQLog.Routes = append(cfg.EQLog.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Regex:        `(\w+) says to general, '(.*)'`,
			NameIndex:    1,
			MessageIndex: 2,
		},
		Target:         "discord",
		ChannelID:      "general",
		MessagePattern: "{{.Name}} **OOC**: {{.Message}}",
	})
	cfg.EQLog.Routes = append(cfg.EQLog.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Regex:        `(\w+) shouts, '(.*)'`,
			NameIndex:    1,
			MessageIndex: 2,
		},
		Target:         "discord",
		ChannelID:      "shout",
		MessagePattern: "{{.Name}} **SHOUT**: {{.Message}}",
	})

	cfg.PEQEditor.SQL.Routes = append(cfg.EQLog.Routes, Route{
		IsEnabled: true,
		Trigger: Trigger{
			Regex:        `(.*)`,
			NameIndex:    0,
			MessageIndex: 1,
		},
		Target:         "discord",
		ChannelID:      "peqeditor",
		MessagePattern: "{{.Name}} **OOC**: {{.Message}}",
	})

	cfg.PEQEditor.SQL.Path = "/var/www/peq/peqphpeditor/logs"
	cfg.PEQEditor.SQL.FilePattern = "sql_log_{{.Month}}-{{.Year}}.sql"

	cfg.SQLReport.Host = "127.0.0.1:3306"
	cfg.SQLReport.Username = "eqemu"
	cfg.SQLReport.Password = "eqemu"
	cfg.SQLReport.Database = "eqemu"
	return cfg
}
