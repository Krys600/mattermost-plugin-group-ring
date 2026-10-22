package main

import (
	"reflect"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/pkg/errors"

	"github.com/utaku/mattermost-plugin-group-ring/server/ring"
)

const (
	defaultRingTimeoutSeconds = 30
	minRingTimeoutSeconds     = 5
	maxRingTimeoutSeconds     = 300
	defaultMaxRecipients      = 50
	maxMaxRecipients          = 1000
	maxRateLimitPerMinute     = 60

	// fallbackDelay is how long the server waits for a webapp to acknowledge a ring before it
	// sends the fallback direct message.
	fallbackDelay = 6 * time.Second
)

// configuration captures the plugin's external configuration as exposed in the Mattermost server
// configuration. Any public fields are deserialized from the Mattermost server configuration in
// OnConfigurationChange; defaults come from plugin.json.
//
// As plugins are inherently concurrent (hooks being called asynchronously), and the plugin
// configuration can change at any time, access to the configuration must be synchronized. The
// strategy used in this plugin is to guard a pointer to the configuration, and clone the entire
// struct whenever it changes.
type configuration struct {
	Enabled              bool
	RingTimeoutSeconds   int
	MaxRecipients        int
	ConfirmThreshold     int
	RateLimitPerMinute   int
	AllowPublicChannels  bool
	AllowPrivateChannels bool
	AllowDirectMessages  bool
	AllowGroupMessages   bool
	AllowedCallers       string
	DNDBehavior          string
	DeferToNativeRinging bool
}

// Clone shallow copies the configuration. The struct has no reference types.
func (c *configuration) Clone() *configuration {
	clone := *c
	return &clone
}

// ringConfig converts the settings into the ring service configuration, clamping out-of-range
// values to safe defaults.
func (c *configuration) ringConfig() ring.Config {
	timeout := c.RingTimeoutSeconds
	switch {
	case timeout <= 0:
		timeout = defaultRingTimeoutSeconds
	case timeout < minRingTimeoutSeconds:
		timeout = minRingTimeoutSeconds
	case timeout > maxRingTimeoutSeconds:
		timeout = maxRingTimeoutSeconds
	}

	maxRecipients := c.MaxRecipients
	switch {
	case maxRecipients <= 0:
		maxRecipients = defaultMaxRecipients
	case maxRecipients > maxMaxRecipients:
		maxRecipients = maxMaxRecipients
	}

	threshold := max(c.ConfirmThreshold, 0)
	rateLimit := min(max(c.RateLimitPerMinute, 0), maxRateLimitPerMinute)

	allowedCallers := c.AllowedCallers
	switch allowedCallers {
	case ring.CallersEveryone, ring.CallersChannelAdmins, ring.CallersTeamAdmins, ring.CallersSystemAdmins:
	default:
		allowedCallers = ring.CallersEveryone
	}

	dnd := c.DNDBehavior
	switch dnd {
	case ring.DNDSilent, ring.DNDDMOnly, ring.DNDSkip:
	default:
		dnd = ring.DNDSilent
	}

	return ring.Config{
		Enabled:            c.Enabled,
		RingTimeout:        time.Duration(timeout) * time.Second,
		MaxRecipients:      maxRecipients,
		ConfirmThreshold:   threshold,
		RateLimitPerMinute: rateLimit,
		ChannelTypes: ring.ChannelTypes{
			Public:  c.AllowPublicChannels,
			Private: c.AllowPrivateChannels,
			Direct:  c.AllowDirectMessages,
			Group:   c.AllowGroupMessages,
		},
		AllowedCallers:       allowedCallers,
		DNDBehavior:          dnd,
		DeferToNativeRinging: c.DeferToNativeRinging,
		FallbackDelay:        fallbackDelay,
	}
}

// getConfiguration retrieves the active configuration under lock, making it safe to use
// concurrently. The active configuration may change underneath the client of this method, but
// the struct returned by this API call is considered immutable.
func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

// setConfiguration replaces the active configuration under lock.
//
// Do not call setConfiguration while holding the configurationLock, as sync.Mutex is not
// reentrant. In particular, avoid using the plugin API entirely, as this may in turn trigger a
// hook back into the plugin. If that hook attempts to acquire this lock, a deadlock may occur.
//
// This method panics if setConfiguration is called with the existing configuration. This almost
// certainly means that the configuration was modified without being cloned and may result in
// an unsafe access.
func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		// Ignore assignment if the configuration struct is empty. Go will optimize the
		// allocation for same to point at the same memory address, breaking the check
		// above.
		if reflect.ValueOf(*configuration).NumField() == 0 {
			return
		}

		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

// OnConfigurationChange is invoked when configuration changes may have been made.
func (p *Plugin) OnConfigurationChange() error {
	configuration := new(configuration)

	// Load the public configuration fields from the Mattermost server configuration.
	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	p.setConfiguration(configuration)

	// Let connected webapps refresh their copy of the client configuration.
	p.API.PublishWebSocketEvent(eventConfigChanged, map[string]any{}, &model.WebsocketBroadcast{})

	return nil
}
