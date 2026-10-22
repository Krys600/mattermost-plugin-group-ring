package main

import (
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/pkg/errors"

	"github.com/Krys600/mattermost-plugin-group-ring/server/calls"
	"github.com/Krys600/mattermost-plugin-group-ring/server/command"
	"github.com/Krys600/mattermost-plugin-group-ring/server/i18n"
	"github.com/Krys600/mattermost-plugin-group-ring/server/kvstore"
	"github.com/Krys600/mattermost-plugin-group-ring/server/ring"
)

// eventConfigChanged tells webapps to reload the client configuration.
const eventConfigChanged = "config_changed"

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	// client is the Mattermost server API client.
	client *pluginapi.Client

	// router is the HTTP router for handling API requests.
	router *mux.Router

	// botUserID is the bot that sends fallback direct messages and ephemeral replies.
	botUserID string

	i18n     *i18n.Bundle
	calls    calls.Client
	ring     *ring.Service
	commands *command.Handler

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration
}

// OnActivate is invoked when the plugin is activated. If an error is returned, the plugin will be deactivated.
func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)

	bundle, err := i18n.Load()
	if err != nil {
		return errors.Wrap(err, "failed to load translations")
	}
	p.i18n = bundle

	botUserID, err := p.client.Bot.EnsureBot(&model.Bot{
		Username:    "group-ring",
		DisplayName: "Group Ring",
		Description: "Sends call links when someone rings you and you are not online.",
	})
	if err != nil {
		return errors.Wrap(err, "failed to ensure bot user")
	}
	p.botUserID = botUserID

	p.calls = calls.NewClient(p.API)
	if availErr := p.calls.Available(); availErr != nil {
		p.API.LogWarn("Group Ring needs the Calls plugin; rings are rejected until it is running", "plugin_id", calls.PluginID, "err", availErr.Error())
	} else {
		p.API.LogInfo("Calls plugin detected", "version", p.calls.Version(), "native_ringing", p.calls.NativeRingingEnabled())
	}

	p.ring = ring.NewService(ring.Dependencies{
		API:       p.API,
		Store:     kvstore.New(p.API),
		Calls:     p.calls,
		Translate: p.i18n.T,
		BotUserID: func() string { return p.botUserID },
		SiteURL:   p.siteURL,
		Config:    func() ring.Config { return p.getConfiguration().ringConfig() },
	})
	p.commands = command.NewHandler(p.ring, p.i18n.T)

	if err := p.API.RegisterCommand(&model.Command{
		Trigger:          command.Trigger,
		DisplayName:      "Group Ring",
		Description:      "Ring people to join a call",
		AutoComplete:     true,
		AutoCompleteDesc: "Ring people to join a call in this channel",
		AutoCompleteHint: "[@user ...] | @channel | ~channel | --help",
		AutocompleteData: command.AutocompleteData(),
	}); err != nil {
		return errors.Wrap(err, "failed to register /ring command")
	}

	p.router = p.initRouter()

	return nil
}

func (p *Plugin) siteURL() string {
	cfg := p.API.GetConfig()
	if cfg == nil || cfg.ServiceSettings.SiteURL == nil {
		return ""
	}
	return *cfg.ServiceSettings.SiteURL
}

func (p *Plugin) userLocale(userID string) string {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return i18n.DefaultLocale
	}
	return user.Locale
}

// ExecuteCommand runs /ring. The webapp intercepts the command and calls the REST API instead,
// so this path serves clients without the webapp plugin, such as the mobile apps.
func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	outcome := p.commands.Run(command.Input{
		UserID:    args.UserId,
		ChannelID: args.ChannelId,
		TeamID:    args.TeamId,
		Text:      args.Command,
		Locale:    p.userLocale(args.UserId),
		Source:    command.SourceServerCommand,
	})
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         outcome.Text,
	}, nil
}

// MessageHasBeenPosted observes the Calls "call started" posts to track active calls.
func (p *Plugin) MessageHasBeenPosted(c *plugin.Context, post *model.Post) {
	if p.calls != nil {
		p.calls.ObservePost(post)
	}
}

// MessageHasBeenUpdated observes the Calls post update that marks the end of a call.
func (p *Plugin) MessageHasBeenUpdated(c *plugin.Context, newPost, oldPost *model.Post) {
	if p.calls != nil {
		p.calls.ObservePost(newPost)
	}
}

// ServeHTTP routes plugin HTTP requests.
// The root URL is <siteUrl>/plugins/ch.utaku.group-ring/api/v1/.
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

// See https://developers.mattermost.com/extend/plugins/server/reference/
