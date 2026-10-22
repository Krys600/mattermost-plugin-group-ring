package ring

import (
	"errors"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/utaku/mattermost-plugin-group-ring/server/calls"
)

// API is the subset of the Mattermost plugin API used by the ring service.
// plugin.API satisfies it; tests use a fake.
type API interface {
	GetChannel(channelID string) (*model.Channel, *model.AppError)
	GetChannelByName(teamID, name string, includeDeleted bool) (*model.Channel, *model.AppError)
	GetChannelMember(channelID, userID string) (*model.ChannelMember, *model.AppError)
	GetUsersInChannel(channelID, sortBy string, page, perPage int) ([]*model.User, *model.AppError)
	GetUser(userID string) (*model.User, *model.AppError)
	GetUserByUsername(name string) (*model.User, *model.AppError)
	GetUserStatusesByIds(userIDs []string) ([]*model.Status, *model.AppError)
	GetTeam(teamID string) (*model.Team, *model.AppError)
	GetTeamsForUser(userID string) ([]*model.Team, *model.AppError)
	GetDirectChannel(userID1, userID2 string) (*model.Channel, *model.AppError)
	HasPermissionTo(userID string, permission *model.Permission) bool
	HasPermissionToTeam(userID, teamID string, permission *model.Permission) bool
	HasPermissionToChannel(userID, channelID string, permission *model.Permission) bool
	PublishWebSocketEvent(event string, payload map[string]any, broadcast *model.WebsocketBroadcast)
	SendEphemeralPost(userID string, post *model.Post) *model.Post
	CreatePost(post *model.Post) (*model.Post, *model.AppError)
	GetConfig() *model.Config

	LogDebug(msg string, keyValuePairs ...any)
	LogInfo(msg string, keyValuePairs ...any)
	LogWarn(msg string, keyValuePairs ...any)
	LogError(msg string, keyValuePairs ...any)
}

// ErrNotFound is returned by Store when a record does not exist or has expired.
var ErrNotFound = errors.New("not found")

// Store persists transient ring state. All records expire.
type Store interface {
	// SaveRing creates or overwrites a ring record. It expires RecordRetention after r.ExpiresAt.
	SaveRing(r *Ring) error
	// GetRing returns ErrNotFound when the ring does not exist or has expired.
	GetRing(id string) (*Ring, error)
	// ModifyRing applies fn to the current record and saves it atomically, retrying on conflicts.
	// When fn returns an error the record is left unchanged and the error is returned.
	ModifyRing(id string, fn func(*Ring) error) (*Ring, error)
	// SetLatestRing remembers the most recent ring started by a caller.
	SetLatestRing(callerID, ringID string, ttl time.Duration) error
	// GetLatestRing returns ErrNotFound when the caller has no recent ring.
	GetLatestRing(callerID string) (string, error)
	// TakeRateLimitToken records one ring for userID within a sliding window. It returns false and
	// the time until the next token is available when the limit is reached.
	TakeRateLimitToken(userID string, limit int, window time.Duration, now time.Time) (bool, time.Duration, error)
}

// Translator returns the localized text for a message ID.
type Translator func(locale, id string, params map[string]any) string

// Scheduler runs f after d. It is time.AfterFunc in production.
type Scheduler func(d time.Duration, f func())

// Dependencies groups everything the service needs.
type Dependencies struct {
	API       API
	Store     Store
	Calls     calls.Client
	Translate Translator
	BotUserID func() string
	SiteURL   func() string
	Config    func() Config
	Now       func() time.Time
	AfterFunc Scheduler
	NewID     func() string
}
