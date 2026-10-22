// Package calls integrates with the official Calls plugin (com.mattermost.calls) using only the
// standard plugin API.
//
// The Calls plugin (v1.12) exposes no server-side API that another plugin can use to start a call
// or read call state: its HTTP routes require a user session, and inter-plugin requests carry no
// user. A call starts when a client joins it over the Calls WebSocket protocol, which the webapp of
// this plugin triggers through the Calls webapp. This package therefore:
//
//   - checks that the Calls plugin is installed and running (GetPluginStatus),
//   - reads the Calls settings from the server configuration (native ringing, test mode),
//   - tracks active calls by observing the "custom_calls" posts that Calls creates when a call
//     starts and updates with "end_at" when it ends.
package calls

//go:generate mockgen -destination=mocks/mock_client.go -package=mocks github.com/utaku/mattermost-plugin-group-ring/server/calls Client

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	// PluginID is the ID of the official Calls plugin.
	PluginID = "com.mattermost.calls"
	// CallPostType is the post type Calls uses for the "call started" post.
	CallPostType = "custom_calls"

	activeCallKeyPrefix = "call_"
	// activeCallTTL bounds how long a tracked call survives if its end is never observed.
	activeCallTTL = 24 * time.Hour
)

var (
	// ErrNotInstalled is returned when the Calls plugin is not installed.
	ErrNotInstalled = errors.New("calls plugin is not installed")
	// ErrNotRunning is returned when the Calls plugin is installed but not running.
	ErrNotRunning = errors.New("calls plugin is not running")
)

// Call is what this plugin knows about a running call.
type Call struct {
	ChannelID string `json:"channel_id"`
	// CallID is the Calls plugin's call ID. It is only known once a webapp reported it.
	CallID string `json:"call_id,omitempty"`
	// PostID is the "call started" post, usable as a permalink to join the call.
	PostID   string `json:"post_id,omitempty"`
	ThreadID string `json:"thread_id,omitempty"`
	StartAt  int64  `json:"start_at,omitempty"`
}

// Client is the integration surface with the Calls plugin.
type Client interface {
	// Available returns nil when the Calls plugin is installed and running.
	Available() error
	// Version returns the installed Calls plugin version, or "" when unknown.
	Version() string
	// NativeRingingEnabled reports whether the Calls "Enable call ringing" setting is on.
	NativeRingingEnabled() bool
	// TestMode reports whether Calls is explicitly in test mode, where only system admins can start calls.
	TestMode() bool
	// ActiveCall returns the tracked call in a channel, or nil when none is known.
	ActiveCall(channelID string) (*Call, error)
	// SetCallID records the Calls call ID for the active call in a channel.
	SetCallID(channelID, callID string) error
	// ObservePost updates call tracking from a created or updated post.
	ObservePost(post *model.Post)
}

// API is the subset of the plugin API used by the client.
type API interface {
	GetPluginStatus(id string) (*model.PluginStatus, *model.AppError)
	GetConfig() *model.Config
	KVSetWithOptions(key string, value []byte, options model.PluginKVSetOptions) (bool, *model.AppError)
	KVGet(key string) ([]byte, *model.AppError)
	KVDelete(key string) *model.AppError
	LogDebug(msg string, keyValuePairs ...any)
	LogWarn(msg string, keyValuePairs ...any)
}

type client struct {
	api API
}

// NewClient returns a Client backed by the plugin API.
func NewClient(api API) Client {
	return &client{api: api}
}

func (c *client) status() (*model.PluginStatus, error) {
	status, appErr := c.api.GetPluginStatus(PluginID)
	if appErr != nil {
		return nil, ErrNotInstalled
	}
	return status, nil
}

func (c *client) Available() error {
	status, err := c.status()
	if err != nil {
		return err
	}
	if status.State != model.PluginStateRunning {
		return ErrNotRunning
	}
	return nil
}

func (c *client) Version() string {
	status, err := c.status()
	if err != nil {
		return ""
	}
	return status.Version
}

// setting reads a Calls plugin setting from the server configuration. Keys are stored in lower
// case by the System Console, so the lookup is case-insensitive.
func (c *client) setting(key string) (any, bool) {
	cfg := c.api.GetConfig()
	if cfg == nil {
		return nil, false
	}
	settings, ok := cfg.PluginSettings.Plugins[PluginID]
	if !ok {
		return nil, false
	}
	for k, v := range settings {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

func (c *client) boolSetting(key string) (value, found bool) {
	raw, ok := c.setting(key)
	if !ok {
		return false, false
	}
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		return strings.EqualFold(v, "true"), true
	}
	return false, false
}

func (c *client) NativeRingingEnabled() bool {
	// The Calls default for EnableRinging is false.
	enabled, _ := c.boolSetting("EnableRinging")
	return enabled
}

func (c *client) TestMode() bool {
	// DefaultEnabled=false is test mode. Only an explicit false counts, because the default
	// differs between deployment types.
	enabled, found := c.boolSetting("DefaultEnabled")
	return found && !enabled
}

func activeCallKey(channelID string) string {
	return activeCallKeyPrefix + channelID
}

func (c *client) ActiveCall(channelID string) (*Call, error) {
	data, appErr := c.api.KVGet(activeCallKey(channelID))
	if appErr != nil {
		return nil, appErr
	}
	if data == nil {
		return nil, nil
	}
	var call Call
	if err := json.Unmarshal(data, &call); err != nil {
		return nil, err
	}
	return &call, nil
}

func (c *client) saveCall(call *Call) error {
	data, err := json.Marshal(call)
	if err != nil {
		return err
	}
	if _, appErr := c.api.KVSetWithOptions(activeCallKey(call.ChannelID), data, model.PluginKVSetOptions{
		ExpireInSeconds: int64(activeCallTTL.Seconds()),
	}); appErr != nil {
		return appErr
	}
	return nil
}

func (c *client) SetCallID(channelID, callID string) error {
	if !model.IsValidId(channelID) || !model.IsValidId(callID) {
		return errors.New("invalid channel or call id")
	}
	call, err := c.ActiveCall(channelID)
	if err != nil {
		return err
	}
	if call == nil {
		call = &Call{ChannelID: channelID}
	}
	call.CallID = callID
	return c.saveCall(call)
}

func (c *client) ObservePost(post *model.Post) {
	if post == nil || post.Type != CallPostType {
		return
	}

	if propInt64(post.GetProp("end_at")) > 0 {
		current, err := c.ActiveCall(post.ChannelId)
		if err != nil {
			c.api.LogWarn("Failed to read tracked call", "channel_id", post.ChannelId, "err", err.Error())
			return
		}
		if current != nil && current.PostID != "" && current.PostID != post.Id {
			// A newer call already replaced this one.
			return
		}
		if appErr := c.api.KVDelete(activeCallKey(post.ChannelId)); appErr != nil {
			c.api.LogWarn("Failed to clear tracked call", "channel_id", post.ChannelId, "err", appErr.Error())
		}
		return
	}

	current, err := c.ActiveCall(post.ChannelId)
	if err != nil {
		c.api.LogWarn("Failed to read tracked call", "channel_id", post.ChannelId, "err", err.Error())
	}
	if current != nil && current.PostID == post.Id {
		// Already tracked; an update of a running call post.
		return
	}

	threadID := post.RootId
	if threadID == "" {
		threadID = post.Id
	}
	call := &Call{
		ChannelID: post.ChannelId,
		PostID:    post.Id,
		ThreadID:  threadID,
		StartAt:   propInt64(post.GetProp("start_at")),
	}
	// Keep a call ID reported before the post was observed.
	if current != nil && current.PostID == "" {
		call.CallID = current.CallID
	}
	if err := c.saveCall(call); err != nil {
		c.api.LogWarn("Failed to track call", "channel_id", post.ChannelId, "err", err.Error())
		return
	}
	c.api.LogDebug("Tracking call", "channel_id", post.ChannelId, "post_id", post.Id)
}

// propInt64 converts a numeric post prop, which may arrive as int64 or float64 depending on
// whether it went through JSON.
func propInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
