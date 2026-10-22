package ring

import (
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// WebSocket event names. The server prefixes them with "custom_<plugin id>_".
const (
	EventIncomingRing = "incoming_ring"
	EventRingUpdated  = "ring_updated"
	EventRingStop     = "ring_stop"
)

// Caller policies for the AllowedCallers setting.
const (
	CallersEveryone      = "everyone"
	CallersChannelAdmins = "channel_admins"
	CallersTeamAdmins    = "team_admins"
	CallersSystemAdmins  = "system_admins"
)

// Behaviors for recipients whose status is Do Not Disturb.
const (
	DNDSilent = "silent"
	DNDDMOnly = "dm_only"
	DNDSkip   = "skip"
)

// DeliveryMode describes how a recipient is notified.
type DeliveryMode string

const (
	// DeliveryRing shows the incoming-call modal with a ringtone.
	DeliveryRing DeliveryMode = "ring"
	// DeliverySilent shows the incoming-call modal without sound.
	DeliverySilent DeliveryMode = "silent"
	// DeliveryDM only sends a direct message from the bot.
	DeliveryDM DeliveryMode = "dm"
)

// RecipientStatus is the lifecycle state of one recipient within a ring.
type RecipientStatus string

const (
	StatusPending   RecipientStatus = "pending"
	StatusDelivered RecipientStatus = "delivered"
	StatusAccepted  RecipientStatus = "accepted"
	StatusDeclined  RecipientStatus = "declined"
	StatusDismissed RecipientStatus = "dismissed"
	StatusMissed    RecipientStatus = "missed"
	StatusCancelled RecipientStatus = "cancelled"
)

// IsFinal reports whether the recipient no longer needs to be rung.
func (s RecipientStatus) IsFinal() bool {
	switch s {
	case StatusAccepted, StatusDeclined, StatusDismissed, StatusMissed, StatusCancelled:
		return true
	}
	return false
}

// Action is a response sent by a recipient's client.
type Action string

const (
	// ActionAck confirms that a webapp session received the ring. It suppresses the fallback DM.
	ActionAck     Action = "ack"
	ActionAccept  Action = "accept"
	ActionDecline Action = "decline"
	ActionDismiss Action = "dismiss"
	// ActionMissed is sent when the ring timed out on the client.
	ActionMissed Action = "missed"
)

// ParseAction validates a client-supplied action name.
func ParseAction(s string) (Action, bool) {
	switch a := Action(s); a {
	case ActionAck, ActionAccept, ActionDecline, ActionDismiss, ActionMissed:
		return a, true
	}
	return "", false
}

// SkipReason explains why a requested user is not rung.
type SkipReason string

const (
	SkipNotMember SkipReason = "not_member"
	SkipNotFound  SkipReason = "not_found"
	SkipBot       SkipReason = "bot"
	SkipInactive  SkipReason = "inactive"
	SkipDND       SkipReason = "dnd"
)

// ChannelTypes lists which channel types allow ringing.
type ChannelTypes struct {
	Public  bool
	Private bool
	Direct  bool
	Group   bool
}

// Allows reports whether rings are allowed in a channel of the given type.
func (c ChannelTypes) Allows(t model.ChannelType) bool {
	switch t {
	case model.ChannelTypeOpen:
		return c.Public
	case model.ChannelTypePrivate:
		return c.Private
	case model.ChannelTypeDirect:
		return c.Direct
	case model.ChannelTypeGroup:
		return c.Group
	}
	return false
}

// Config holds the validated settings that drive the ring service.
type Config struct {
	Enabled              bool
	RingTimeout          time.Duration
	MaxRecipients        int
	ConfirmThreshold     int
	RateLimitPerMinute   int
	ChannelTypes         ChannelTypes
	AllowedCallers       string
	DNDBehavior          string
	DeferToNativeRinging bool
	// FallbackDelay is how long the service waits for a webapp acknowledgement
	// before it sends the fallback direct message.
	FallbackDelay time.Duration
}

// Recipient is the per-user state stored with a ring.
type Recipient struct {
	Mode   DeliveryMode    `json:"mode"`
	Status RecipientStatus `json:"status"`
	DMSent bool            `json:"dm_sent,omitempty"`
}

// Ring is the transient record of one ring, kept in the KV store with an expiry.
type Ring struct {
	ID         string                `json:"id"`
	CallerID   string                `json:"caller_id"`
	ChannelID  string                `json:"channel_id"`
	TeamID     string                `json:"team_id,omitempty"`
	CallID     string                `json:"call_id,omitempty"`
	CallPostID string                `json:"call_post_id,omitempty"`
	CreatedAt  int64                 `json:"created_at"`
	ExpiresAt  int64                 `json:"expires_at"`
	Cancelled  bool                  `json:"cancelled,omitempty"`
	Recipients map[string]*Recipient `json:"recipients"`
}

// Expired reports whether the ring timed out at the given time.
func (r *Ring) Expired(now time.Time) bool {
	return now.UnixMilli() >= r.ExpiresAt
}

// Request describes who should be rung, independent of the entry point (slash command or webapp).
type Request struct {
	CallerID string
	// ChannelID is the channel the request was made from.
	ChannelID string
	// TeamID is the caller's current team, used to resolve ~channel-name.
	TeamID string
	// Usernames are explicitly mentioned users (without the leading @).
	Usernames []string
	// UserIDs are explicitly selected users, for example from the webapp modal.
	UserIDs []string
	// WholeChannel rings every member of the target channel.
	WholeChannel bool
	// TargetChannelName rings the members of a named channel in TeamID instead of ChannelID.
	TargetChannelName string
	// Confirmed is true when the caller already confirmed a large ring.
	Confirmed bool
	// DryRun resolves recipients and checks permissions without ringing anyone.
	DryRun bool
	// CallActiveHint is set by the webapp when it knows a call is running in the channel.
	CallActiveHint bool
	// GroupCallsAllowedHint is the Calls plugin's own "GroupCallsAllowed" client setting, read by
	// the webapp. Calls v1 only allows calls outside DMs with a license or when the server sets
	// MM_CALLS_GROUP_CALLS_ALLOWED=true. Nil means unknown (for example from the mobile apps).
	GroupCallsAllowedHint *bool
	// Source is "command" or "webapp", used for audit logging only.
	Source string
}

// Skipped is a requested user who is not rung.
type Skipped struct {
	UserID   string     `json:"user_id,omitempty"`
	Username string     `json:"username"`
	Reason   SkipReason `json:"reason"`
}

// Result describes the outcome of a ring request.
type Result struct {
	RingID         string         `json:"ring_id,omitempty"`
	Channel        *model.Channel `json:"-"`
	ChannelID      string         `json:"channel_id"`
	ChannelName    string         `json:"channel_name"`
	TeamID         string         `json:"team_id,omitempty"`
	CallActive     bool           `json:"call_active"`
	RecipientCount int            `json:"recipient_count"`
	Rung           []string       `json:"rung"`
	Silent         []string       `json:"silent"`
	DMOnly         []string       `json:"dm_only"`
	Skipped        []Skipped      `json:"skipped"`
	// NeedsConfirmation is set when the ring exceeds the confirmation threshold
	// and the request was not confirmed. Nobody was rung.
	NeedsConfirmation bool `json:"needs_confirmation"`
	// DeferredToNative is set when the Calls plugin's native ringing handles this ring.
	DeferredToNative bool `json:"deferred_to_native"`
	DryRun           bool `json:"dry_run"`
	TimeoutSeconds   int  `json:"timeout_seconds"`
}
