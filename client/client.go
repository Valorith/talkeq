package client

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/xackery/log"
	"github.com/xackery/talkeq/channel"
	"github.com/xackery/talkeq/config"
	"github.com/xackery/talkeq/database"
	"github.com/xackery/talkeq/discord"
	"github.com/xackery/talkeq/eqlog"
	"github.com/xackery/talkeq/nats"
	"github.com/xackery/talkeq/peqeditorsql"
	"github.com/xackery/talkeq/sqlreport"
	"github.com/xackery/talkeq/telnet"
)

// Client wraps all talking endpoints
type Client struct {
	ctx          context.Context
	cancel       context.CancelFunc
	config       *config.Config
	discord      *discord.Discord
	telnet       *telnet.Telnet
	eqlog        *eqlog.EQLog
	nats         *nats.Nats
	sqlreport    *sqlreport.SQLReport
	peqeditorsql *peqeditorsql.PEQEditorSQL
}

// New creates a new client
func New(ctx context.Context) (*Client, error) {
	var err error
	ctx, cancel := context.WithCancel(ctx)
	c := Client{
		ctx:    ctx,
		cancel: cancel,
	}
	c.config, err = config.NewConfig(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "config")
	}

	if c.config.IsKeepAliveEnabled && c.config.KeepAliveRetry.Seconds() < 2 {
		c.config.KeepAliveRetry.Duration = 10 * time.Second
		//return nil, fmt.Errorf("keep_alive_retry must be greater than 2s")
	}

	userManager, err := database.NewUserManager(ctx, c.config)
	if err != nil {
		return nil, errors.Wrap(err, "usermanager")
	}

	guildManager, err := database.NewGuildManager(ctx, c.config)
	if err != nil {
		return nil, errors.Wrap(err, "guildmanager")
	}

	c.discord, err = discord.New(ctx, c.config.Discord, userManager, guildManager)
	if err != nil {
		return nil, errors.Wrap(err, "discord")
	}

	err = c.discord.Subscribe(ctx, c.onMessage)
	if err != nil {
		return nil, errors.Wrap(err, "discord subscribe")
	}

	c.telnet, err = telnet.New(ctx, c.config.Telnet)
	if err != nil {
		return nil, errors.Wrap(err, "telnet")
	}

	c.sqlreport, err = sqlreport.New(ctx, c.config.SQLReport, c.discord)
	if err != nil {
		return nil, errors.Wrap(err, "sqlreport")
	}

	err = c.telnet.Subscribe(ctx, c.onMessage)
	if err != nil {
		return nil, errors.Wrap(err, "telnet subscribe")
	}

	c.eqlog, err = eqlog.New(ctx, c.config.EQLog)
	if err != nil {
		return nil, errors.Wrap(err, "eqlog")
	}

	err = c.eqlog.Subscribe(ctx, c.onMessage)
	if err != nil {
		return nil, errors.Wrap(err, "eqlog subscribe")
	}

	c.peqeditorsql, err = peqeditorsql.New(ctx, c.config.PEQEditor.SQL)
	if err != nil {
		return nil, errors.Wrap(err, "peqeditorsql")
	}

	err = c.peqeditorsql.Subscribe(ctx, c.onMessage)
	if err != nil {
		return nil, errors.Wrap(err, "peqeditorsql subscribe")
	}

	c.nats, err = nats.New(ctx, c.config.Nats, guildManager)
	if err != nil {
		return nil, errors.Wrap(err, "nats")
	}

	err = c.nats.Subscribe(ctx, c.onMessage)
	if err != nil {
		return nil, errors.Wrap(err, "nats subscribe")
	}
	return &c, nil
}

// Connect attempts to connect to all enabled endpoints
func (c *Client) Connect(ctx context.Context) error {
	log := log.New()
	err := c.discord.Connect(ctx)
	if err != nil {
		if !c.config.IsKeepAliveEnabled {
			return errors.Wrap(err, "discord connect")
		}
		log.Warn().Err(err).Msg("discord connect")
	}

	err = c.telnet.Connect(ctx)
	if err != nil {
		if !c.config.IsKeepAliveEnabled {
			return errors.Wrap(err, "telnet connect")
		}
		log.Warn().Err(err).Msg("telnet connect")
	}

	err = c.sqlreport.Connect(ctx)
	if err != nil {
		if !c.config.IsKeepAliveEnabled {
			return errors.Wrap(err, "sqlreport connect")
		}
		log.Warn().Err(err).Msg("sqlreport connect")
	}

	err = c.eqlog.Connect(ctx)
	if err != nil {
		if !c.config.IsKeepAliveEnabled {
			return errors.Wrap(err, "eqlog connect")
		}
		log.Warn().Err(err).Msg("eqlog connect")
	}

	err = c.peqeditorsql.Connect(ctx)
	if err != nil {
		if !c.config.IsKeepAliveEnabled {
			return errors.Wrap(err, "peqeditorsql connect")
		}
		log.Warn().Err(err).Msg("peqeditorsql connect")
	}

	err = c.nats.Connect(ctx)
	if err != nil {
		if !c.config.IsKeepAliveEnabled {
			return errors.Wrap(err, "nats connect")
		}
		log.Warn().Err(err).Msg("nats connect")
	}

	go c.loop(ctx)
	return nil
}

func (c *Client) loop(ctx context.Context) {
	log := log.New()
	var err error
	go func() {
		var err error
		var online int
		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("status loop exit, context done")
				return
			default:
			}
			if c.config.Telnet.IsEnabled && c.config.Discord.IsEnabled {
				online, err = c.telnet.Who(ctx)
				if err != nil {
					log.Warn().Err(err).Msg("telnet who")
				}
				err = c.discord.StatusUpdate(ctx, online, "")
				if err != nil {
					log.Warn().Err(err).Msg("discord status update")
				}
			}

			time.Sleep(60 * time.Second)
		}
	}()
	if !c.config.IsKeepAliveEnabled {
		log.Debug().Msg("keep_alive disabled in config, exiting client loop")
		return
	}
	for {
		select {
		case <-ctx.Done():
			log.Debug().Msg("client loop exit, context done")
			return
		default:
		}
		time.Sleep(c.config.KeepAliveRetry.Duration)
		if c.config.Discord.IsEnabled && !c.discord.IsConnected() {
			log.Info().Msg("attempting to reconnect to discord")
			err = c.discord.Connect(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("discord connect")
			}
		}
		if c.config.Telnet.IsEnabled && !c.telnet.IsConnected() {
			log.Info().Msg("attempting to reconnect to telnet")
			err = c.telnet.Connect(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("telnet connect")
			}
		}
		if c.config.SQLReport.IsEnabled && !c.sqlreport.IsConnected() {
			log.Info().Msg("attempting to reconnect to sqlreport")
			err = c.sqlreport.Connect(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("sqlreport connect")
			}
		}
	}
}

func (c *Client) onMessage(source string, author string, channelID int, message string, optional string) {
	var err error
	log := log.New()
	endpoints := "none"
	switch source {
	case "peqeditorsql":
		if !c.config.Discord.IsEnabled {
			log.Info().Msgf("[%s->none] %s %s: %s", source, author, channel.ToString(channelID), message)
			return
		}
		err = c.discord.Send(context.Background(), source, author, channelID, message, optional)
		if err != nil {
			log.Warn().Err(err).Msg("discord send")
		} else {
			if endpoints == "none" {
				endpoints = "discord"
			} else {
				endpoints += ",discord"
			}
		}
		log.Info().Msgf("[%s->%s] %s %s: %s", source, endpoints, author, channel.ToString(channelID), message)
	case "telnet":
		if !c.config.Discord.IsEnabled {
			log.Info().Msgf("[%s->none] %s %s: %s", source, author, channel.ToString(channelID), message)
			return
		}
		err = c.discord.Send(context.Background(), source, author, channelID, message, optional)
		if err != nil {
			log.Warn().Err(err).Msg("discord send")
		} else {
			if endpoints == "none" {
				endpoints = "discord"
			} else {
				endpoints += ",discord"
			}
		}
		log.Info().Msgf("[%s->%s] %s %s: %s", source, endpoints, author, channel.ToString(channelID), message)
	case "nats":
		if !c.config.Discord.IsEnabled {
			log.Info().Msgf("[%s->none] %s %s: %s", source, author, channel.ToString(channelID), message)
			return
		}
		err = c.discord.Send(context.Background(), source, author, channelID, message, optional)
		if err != nil {
			log.Warn().Err(err).Msg("discord send")
		} else {
			if endpoints == "none" {
				endpoints = "discord"
			} else {
				endpoints += ",discord"
			}
		}
		log.Info().Msgf("[%s->%s] %s %s: %s", source, endpoints, author, channel.ToString(channelID), message)
	case "discord":
		isSent := false
		if c.config.Telnet.IsEnabled {
			err = c.telnet.Send(context.Background(), source, author, channelID, message, optional)
			if err != nil {
				log.Warn().Err(err).Msg("telnet send")
			} else {
				if endpoints == "none" {
					endpoints = "telnet"
				} else {
					endpoints += ",telnet"
				}
			}
			isSent = true
		}
		if c.config.Nats.IsEnabled {
			err = c.nats.Send(context.Background(), source, author, channelID, message, optional)
			if err != nil {
				log.Warn().Err(err).Msg("nats send")
			} else {
				if endpoints == "none" {
					endpoints = "nats"
				} else {
					endpoints += ",nats"
				}
			}

			isSent = true
		}

		if !isSent {
			log.Info().Msgf("[%s->none] %s %s: %s", source, author, channel.ToString(channelID), message)
			return
		}
		log.Info().Msgf("[%s->%s] %s %s: %s", source, endpoints, author, channel.ToString(channelID), message)

	case "eqlog":
		if !c.config.Discord.IsEnabled {
			log.Info().Msgf("[%s->none] %s %s: %s", source, author, channel.ToString(channelID), message)
			return
		}
		err = c.discord.Send(context.Background(), source, author, channelID, message, optional)
		if err != nil {
			log.Warn().Err(err).Msg("discord send")
		} else {
			if endpoints == "none" {
				endpoints = "discord"
			} else {
				endpoints += ",discord"
			}
		}
		log.Info().Msgf("[%s->%s] %s %s: %s", source, endpoints, author, channel.ToString(channelID), message)
	default:
		log.Warn().Str("source", source).Str("author", author).Int("channelID", channelID).Str("message", message).Msg("unknown source")
	}
}

// Disconnect attempts to gracefully disconnect all enabled endpoints
func (c *Client) Disconnect(ctx context.Context) error {
	err := c.discord.Disconnect(ctx)
	if err != nil {
		return errors.Wrap(err, "discord")
	}
	c.cancel()
	return nil
}
