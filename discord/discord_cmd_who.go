package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/xackery/talkeq/characterdb"
	"github.com/xackery/talkeq/tlog"
)

func (t *Discord) whoRegister() error {
	tlog.Debugf("[discord] registering who command")
	_, err := t.conn.ApplicationCommandCreate(t.conn.State.User.ID, t.config.ServerID, &discordgo.ApplicationCommand{
		Name:        "who",
		Description: "Get a list of players on server, optionally filter by zone or name",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "filter",
				Description: "Filter by player name or zone (leave empty for all players)",
				Required:    false,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("whoRegister commandCreate: %w", err)
	}
	return nil
}

func (t *Discord) who(s *discordgo.Session, i *discordgo.InteractionCreate) (content string, err error) {
	appCmdData := i.ApplicationCommandData()
	/*	if len(appCmdData.Options) == 0 {
		content = "usage: /who all, /who <name>"
		return
	}*/
	arg := ""
	if len(appCmdData.Options) > 0 {
		arg = fmt.Sprintf("%s", i.ApplicationCommandData().Options[0].Value)
		if arg == "all" {
			arg = ""
		}
	}

	content = characterdb.CharactersOnline(arg)
	return
}
