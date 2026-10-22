package ring_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/utaku/mattermost-plugin-group-ring/server/calls"
	"github.com/utaku/mattermost-plugin-group-ring/server/calls/mocks"
	"github.com/utaku/mattermost-plugin-group-ring/server/kvstore"
	"github.com/utaku/mattermost-plugin-group-ring/server/ring"
)

type env struct {
	api       *fakeAPI
	kv        *fakeKV
	calls     *mocks.MockClient
	scheduler *fakeScheduler
	clock     *fakeClock
	cfg       ring.Config
	svc       *ring.Service

	bot     *model.User
	team    *model.Team
	caller  *model.User
	alice   *model.User
	bob     *model.User
	carol   *model.User
	outside *model.User
	robot   *model.User
	channel *model.Channel
}

func defaultConfig() ring.Config {
	return ring.Config{
		Enabled:              true,
		RingTimeout:          30 * time.Second,
		MaxRecipients:        50,
		ConfirmThreshold:     10,
		RateLimitPerMinute:   3,
		ChannelTypes:         ring.ChannelTypes{Public: true, Private: true, Direct: true, Group: true},
		AllowedCallers:       ring.CallersEveryone,
		DNDBehavior:          ring.DNDSilent,
		DeferToNativeRinging: true,
		FallbackDelay:        6 * time.Second,
	}
}

// newEnv builds a team with a public channel "town-square" holding the caller, alice, bob,
// carol and a bot. "outside" exists but is not a channel member.
func newEnv(t *testing.T) *env {
	t.Helper()
	ctrl := gomock.NewController(t)

	e := &env{
		api:       newFakeAPI(),
		kv:        newFakeKV(),
		calls:     mocks.NewMockClient(ctrl),
		scheduler: &fakeScheduler{},
		clock:     &fakeClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)},
		cfg:       defaultConfig(),
	}
	e.bot = e.api.addUser("group-ring", asBot)
	e.team = e.api.addTeam("acme")
	e.caller = e.api.addUser("caller")
	e.alice = e.api.addUser("alice")
	e.bob = e.api.addUser("bob")
	e.carol = e.api.addUser("carol")
	e.outside = e.api.addUser("outside")
	e.robot = e.api.addUser("robot", asBot)
	e.channel = e.api.addChannel(e.team.Id, "town-square", model.ChannelTypeOpen, e.caller, e.alice, e.bob, e.carol, e.robot)

	// Default Calls behavior: running, no active call, native ringing off, not in test mode.
	e.calls.EXPECT().Available().Return(nil).AnyTimes()
	e.calls.EXPECT().TestMode().Return(false).AnyTimes()
	e.calls.EXPECT().NativeRingingEnabled().Return(false).AnyTimes()
	e.calls.EXPECT().ActiveCall(gomock.Any()).Return(nil, nil).AnyTimes()

	store := kvstore.New(e.kv)
	e.svc = ring.NewService(ring.Dependencies{
		API:       e.api,
		Store:     store,
		Calls:     e.calls,
		Translate: func(_, id string, params map[string]any) string { return fmt.Sprintf("%s %v", id, params) },
		BotUserID: func() string { return e.bot.Id },
		SiteURL:   func() string { return "https://chat.example.com" },
		Config:    func() ring.Config { return e.cfg },
		Now:       e.clock.Now,
		AfterFunc: e.scheduler.AfterFunc,
		NewID:     model.NewId,
	})
	return e
}

// newEnvWithCalls is like newEnv but lets the test set up the Calls mock itself.
func newEnvWithCalls(t *testing.T, setup func(m *mocks.MockClient)) *env {
	t.Helper()
	ctrl := gomock.NewController(t)
	e := newEnv(t)
	e.calls = mocks.NewMockClient(ctrl)
	setup(e.calls)
	e.svc = ring.NewService(ring.Dependencies{
		API:       e.api,
		Store:     kvstore.New(e.kv),
		Calls:     e.calls,
		Translate: func(_, id string, params map[string]any) string { return fmt.Sprintf("%s %v", id, params) },
		BotUserID: func() string { return e.bot.Id },
		SiteURL:   func() string { return "https://chat.example.com" },
		Config:    func() ring.Config { return e.cfg },
		Now:       e.clock.Now,
		AfterFunc: e.scheduler.AfterFunc,
	})
	return e
}

func (e *env) request(usernames ...string) ring.Request {
	return ring.Request{
		CallerID:  e.caller.Id,
		ChannelID: e.channel.Id,
		TeamID:    e.team.Id,
		Usernames: usernames,
		Source:    "test",
	}
}

// errDetails exposes the details of a *ring.Error without being an error value itself.
type errDetails struct {
	Params  map[string]any
	Skipped []ring.Skipped
}

func requireCode(t *testing.T, err error, code ring.ErrorCode) errDetails {
	t.Helper()
	require.Error(t, err)
	var e *ring.Error
	require.True(t, errors.As(err, &e), "expected *ring.Error, got %T: %v", err, err)
	require.Equal(t, code, e.Code)
	return errDetails{Params: e.Params, Skipped: e.Skipped}
}

func sortedIDs(users ...*model.User) []string {
	ids := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
	}
	return sortStrings(ids)
}

func sortStrings(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestStartRingsListedMembers(t *testing.T) {
	e := newEnv(t)

	result, err := e.svc.Start(e.request("alice", "@bob"))
	require.NoError(t, err)

	assert.NotEmpty(t, result.RingID)
	assert.Equal(t, 2, result.RecipientCount)
	assert.ElementsMatch(t, []string{"alice", "bob"}, result.Rung)
	assert.Equal(t, sortedIDs(e.alice, e.bob), e.api.recipientsOf(ring.EventIncomingRing))

	payload := e.api.eventsFor(ring.EventIncomingRing)[0].Payload
	assert.Equal(t, result.RingID, payload["ring_id"])
	assert.Equal(t, e.caller.Id, payload["caller_id"])
	assert.Equal(t, "caller", payload["caller_username"])
	assert.Equal(t, e.channel.Id, payload["channel_id"])
	assert.Equal(t, false, payload["silent"])
	assert.Equal(t, 30, payload["timeout_seconds"])
	assert.Contains(t, payload, "call_id")

	t.Run("audit log has no message content and counts recipients", func(t *testing.T) {
		var found bool
		for _, l := range e.api.logs {
			if l.Level == "info" && l.Msg == "Ring started" {
				found = true
				assert.Equal(t, e.caller.Id, l.KV["caller_id"])
				assert.Equal(t, e.channel.Id, l.KV["channel_id"])
				assert.Equal(t, 2, l.KV["recipient_count"])
			}
		}
		assert.True(t, found)
	})
}

func TestStartFiltersRecipients(t *testing.T) {
	e := newEnv(t)
	gone := e.api.addUser("gone", deactivated)
	e.api.join(e.channel, gone)

	result, err := e.svc.Start(e.request("caller", "alice", "robot", "outside", "nobody", "gone", "alice"))
	require.NoError(t, err)

	assert.Equal(t, []string{"alice"}, result.Rung, "caller, bots, non-members, unknown and deactivated users are never rung")
	reasons := map[string]ring.SkipReason{}
	for _, s := range result.Skipped {
		reasons[s.Username] = s.Reason
	}
	assert.Equal(t, map[string]ring.SkipReason{
		"robot":   ring.SkipBot,
		"outside": ring.SkipNotMember,
		"nobody":  ring.SkipNotFound,
		"gone":    ring.SkipInactive,
	}, reasons)
	assert.Equal(t, sortedIDs(e.alice), e.api.recipientsOf(ring.EventIncomingRing))
}

func TestStartNeverRingsNonMembersByID(t *testing.T) {
	e := newEnv(t)
	req := e.request()
	req.UserIDs = []string{e.outside.Id}

	err := requireCode(t, func() error { _, err := e.svc.Start(req); return err }(), ring.ErrNoRecipients)
	require.Len(t, err.Skipped, 1)
	assert.Equal(t, ring.SkipNotMember, err.Skipped[0].Reason)
	assert.Empty(t, e.api.events)
}

func TestStartOnlySelf(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Start(e.request("caller"))
	requireCode(t, err, ring.ErrNoRecipients)
}

func TestStartWholeChannel(t *testing.T) {
	e := newEnv(t)
	req := e.request()
	req.WholeChannel = true

	result, err := e.svc.Start(req)
	require.NoError(t, err)
	assert.Equal(t, 3, result.RecipientCount)
	assert.Equal(t, sortedIDs(e.alice, e.bob, e.carol), e.api.recipientsOf(ring.EventIncomingRing))
	assert.Empty(t, result.Skipped, "bots are dropped silently in channel rings")
}

func TestStartNamedChannel(t *testing.T) {
	e := newEnv(t)
	dev := e.api.addChannel(e.team.Id, "dev", model.ChannelTypePrivate, e.caller, e.carol)

	req := e.request()
	req.TargetChannelName = "~dev"
	result, err := e.svc.Start(req)
	require.NoError(t, err)
	assert.Equal(t, dev.Id, result.ChannelID)
	assert.Equal(t, "dev", result.ChannelName)
	assert.Equal(t, sortedIDs(e.carol), e.api.recipientsOf(ring.EventIncomingRing))

	t.Run("unknown channel", func(t *testing.T) {
		req.TargetChannelName = "missing"
		_, err := e.svc.Start(req)
		requireCode(t, err, ring.ErrChannelNotFound)
	})

	t.Run("caller not a member of the named channel", func(t *testing.T) {
		e.api.addChannel(e.team.Id, "secret", model.ChannelTypePrivate, e.alice)
		req.TargetChannelName = "secret"
		_, err := e.svc.Start(req)
		requireCode(t, err, ring.ErrNotChannelMember)
	})
}

func TestStartLargeChannelPaging(t *testing.T) {
	e := newEnv(t)
	e.cfg.MaxRecipients = 1000
	e.cfg.ConfirmThreshold = 0
	e.cfg.RateLimitPerMinute = 0
	big := e.api.addChannel(e.team.Id, "big", model.ChannelTypeOpen, e.caller)
	for i := range 450 {
		e.api.join(big, e.api.addUser(fmt.Sprintf("user%03d", i)))
	}

	req := e.request()
	req.ChannelID = big.Id
	req.WholeChannel = true
	result, err := e.svc.Start(req)
	require.NoError(t, err)
	assert.Equal(t, 450, result.RecipientCount)
}

func TestStartTooManyRecipients(t *testing.T) {
	e := newEnv(t)
	e.cfg.MaxRecipients = 2
	req := e.request()
	req.WholeChannel = true

	err := requireCode(t, func() error { _, err := e.svc.Start(req); return err }(), ring.ErrTooManyRecipients)
	assert.Equal(t, 2, err.Params["Max"])
	assert.Empty(t, e.api.events)
}

func TestStartRequiresConfirmationAboveThreshold(t *testing.T) {
	e := newEnv(t)
	e.cfg.ConfirmThreshold = 2
	e.cfg.RateLimitPerMinute = 1
	req := e.request()
	req.WholeChannel = true

	result, err := e.svc.Start(req)
	require.NoError(t, err)
	assert.True(t, result.NeedsConfirmation)
	assert.Equal(t, 3, result.RecipientCount)
	assert.Empty(t, result.RingID)
	assert.Empty(t, e.api.events, "nobody is rung before confirmation")

	req.Confirmed = true
	result, err = e.svc.Start(req)
	require.NoError(t, err, "an unconfirmed request does not consume the rate limit")
	assert.False(t, result.NeedsConfirmation)
	assert.Len(t, e.api.eventsFor(ring.EventIncomingRing), 3)
}

func TestStartPermissions(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		e := newEnv(t)
		e.cfg.Enabled = false
		_, err := e.svc.Start(e.request("alice"))
		requireCode(t, err, ring.ErrDisabled)
	})

	t.Run("caller must be a channel member", func(t *testing.T) {
		e := newEnv(t)
		req := e.request("alice")
		req.CallerID = e.outside.Id
		_, err := e.svc.Start(req)
		requireCode(t, err, ring.ErrNotChannelMember)
		assert.Empty(t, e.api.events)
	})

	t.Run("caller must be able to post", func(t *testing.T) {
		e := newEnv(t)
		e.api.canPost[e.channel.Id+":"+e.caller.Id] = false
		_, err := e.svc.Start(e.request("alice"))
		requireCode(t, err, ring.ErrNoPostPermission)
	})

	t.Run("channel type not allowed", func(t *testing.T) {
		e := newEnv(t)
		e.cfg.ChannelTypes.Public = false
		_, err := e.svc.Start(e.request("alice"))
		requireCode(t, err, ring.ErrChannelType)
	})

	t.Run("archived channel", func(t *testing.T) {
		e := newEnv(t)
		e.channel.DeleteAt = 1
		_, err := e.svc.Start(e.request("alice"))
		requireCode(t, err, ring.ErrChannelNotFound)
	})

	t.Run("mixed targets", func(t *testing.T) {
		e := newEnv(t)
		req := e.request("alice")
		req.WholeChannel = true
		_, err := e.svc.Start(req)
		requireCode(t, err, ring.ErrMixedTargets)
	})

	t.Run("no targets", func(t *testing.T) {
		e := newEnv(t)
		_, err := e.svc.Start(e.request())
		requireCode(t, err, ring.ErrInvalidRequest)
	})
}

func TestStartRoleRestriction(t *testing.T) {
	cases := []struct {
		policy  string
		setup   func(e *env)
		allowed bool
	}{
		{ring.CallersEveryone, func(*env) {}, true},
		{ring.CallersChannelAdmins, func(*env) {}, false},
		{ring.CallersChannelAdmins, func(e *env) { e.api.members[e.channel.Id][e.caller.Id].SchemeAdmin = true }, true},
		{ring.CallersChannelAdmins, func(e *env) { e.api.teamAdmins[e.team.Id+":"+e.caller.Id] = true }, true},
		{ring.CallersTeamAdmins, func(e *env) { e.api.members[e.channel.Id][e.caller.Id].SchemeAdmin = true }, false},
		{ring.CallersTeamAdmins, func(e *env) { e.api.teamAdmins[e.team.Id+":"+e.caller.Id] = true }, true},
		{ring.CallersSystemAdmins, func(e *env) { e.api.teamAdmins[e.team.Id+":"+e.caller.Id] = true }, false},
		{ring.CallersSystemAdmins, func(e *env) { e.api.sysAdmins[e.caller.Id] = true }, true},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("%d_%s", i, tc.policy), func(t *testing.T) {
			e := newEnv(t)
			e.cfg.AllowedCallers = tc.policy
			tc.setup(e)
			_, err := e.svc.Start(e.request("alice"))
			if tc.allowed {
				require.NoError(t, err)
			} else {
				requireCode(t, err, ring.ErrRoleNotAllowed)
			}
		})
	}
}

func TestStartRateLimit(t *testing.T) {
	e := newEnv(t)
	e.cfg.RateLimitPerMinute = 3

	for i := range 3 {
		_, err := e.svc.Start(e.request("alice"))
		require.NoError(t, err, "ring %d", i+1)
		e.clock.Advance(10 * time.Second)
	}

	err := requireCode(t, func() error { _, err := e.svc.Start(e.request("alice")); return err }(), ring.ErrRateLimited)
	assert.Equal(t, 30, err.Params["Seconds"], "the oldest ring leaves the window 30 s later")

	t.Run("other callers are not affected", func(t *testing.T) {
		req := e.request("bob")
		req.CallerID = e.alice.Id
		_, err := e.svc.Start(req)
		require.NoError(t, err)
	})

	t.Run("the window slides", func(t *testing.T) {
		e.clock.Advance(31 * time.Second)
		_, err := e.svc.Start(e.request("alice"))
		require.NoError(t, err)
	})

	t.Run("zero disables the limit", func(t *testing.T) {
		e.cfg.RateLimitPerMinute = 0
		for range 5 {
			_, err := e.svc.Start(e.request("alice"))
			require.NoError(t, err)
		}
	})
}

func TestStartRespectsUserStatus(t *testing.T) {
	setup := func(t *testing.T, dnd string) *env {
		e := newEnv(t)
		e.cfg.DNDBehavior = dnd
		e.api.statuses[e.alice.Id] = model.StatusOnline
		e.api.statuses[e.bob.Id] = model.StatusDnd
		e.api.statuses[e.carol.Id] = model.StatusOffline
		return e
	}

	t.Run("silent DND and offline DM", func(t *testing.T) {
		e := setup(t, ring.DNDSilent)
		result, err := e.svc.Start(e.request("alice", "bob", "carol"))
		require.NoError(t, err)

		assert.Equal(t, []string{"alice"}, result.Rung)
		assert.Equal(t, []string{"bob"}, result.Silent)
		assert.Equal(t, []string{"carol"}, result.DMOnly)
		assert.Equal(t, sortedIDs(e.alice, e.bob), e.api.recipientsOf(ring.EventIncomingRing), "offline users get no real-time event")
		for _, ev := range e.api.eventsFor(ring.EventIncomingRing) {
			assert.Equal(t, ev.UserID == e.bob.Id, ev.Payload["silent"])
		}

		// Run the immediate DM task only, not the fallback.
		require.Len(t, e.scheduler.tasks, 2)
		assert.Equal(t, time.Duration(0), e.scheduler.tasks[0].delay)
		e.scheduler.tasks[0].fn()
		assert.Equal(t, sortedIDs(e.carol), e.api.dmRecipients(e.bot.Id))
		assert.Contains(t, e.api.posts[0].Message, "https://chat.example.com/acme/channels/town-square")
	})

	t.Run("DND gets DM only", func(t *testing.T) {
		e := setup(t, ring.DNDDMOnly)
		result, err := e.svc.Start(e.request("alice", "bob", "carol"))
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"bob", "carol"}, result.DMOnly)
		assert.Equal(t, sortedIDs(e.alice), e.api.recipientsOf(ring.EventIncomingRing))
	})

	t.Run("DND skipped", func(t *testing.T) {
		e := setup(t, ring.DNDSkip)
		result, err := e.svc.Start(e.request("alice", "bob"))
		require.NoError(t, err)
		require.Len(t, result.Skipped, 1)
		assert.Equal(t, ring.SkipDND, result.Skipped[0].Reason)
		assert.Equal(t, sortedIDs(e.alice), e.api.recipientsOf(ring.EventIncomingRing))
	})

	t.Run("only DND users skipped means nobody to ring", func(t *testing.T) {
		e := setup(t, ring.DNDSkip)
		_, err := e.svc.Start(e.request("bob"))
		requireCode(t, err, ring.ErrNoRecipients)
	})
}

func TestFallbackDirectMessage(t *testing.T) {
	e := newEnv(t)
	result, err := e.svc.Start(e.request("alice", "bob"))
	require.NoError(t, err)

	require.Len(t, e.scheduler.tasks, 1)
	assert.Equal(t, 6*time.Second, e.scheduler.tasks[0].delay)

	// Alice's webapp acknowledges the ring; Bob has no webapp session.
	require.NoError(t, e.svc.Respond(result.RingID, e.alice.Id, ring.ActionAck))

	e.scheduler.runAll()
	assert.Equal(t, sortedIDs(e.bob), e.api.dmRecipients(e.bot.Id))

	t.Run("runs once", func(t *testing.T) {
		e.svc.SendFallbackMessages(result.RingID)
		assert.Len(t, e.api.posts, 1)
	})
}

func TestFallbackSkippedAfterCancel(t *testing.T) {
	e := newEnv(t)
	result, err := e.svc.Start(e.request("alice"))
	require.NoError(t, err)
	_, err = e.svc.Cancel(e.caller.Id, result.RingID)
	require.NoError(t, err)

	e.scheduler.runAll()
	assert.Empty(t, e.api.posts)
}

func TestFallbackLinksToCallPost(t *testing.T) {
	callPostID := model.NewId()
	e := newEnvWithCalls(t, func(m *mocks.MockClient) {
		m.EXPECT().Available().Return(nil).AnyTimes()
		m.EXPECT().TestMode().Return(false).AnyTimes()
		m.EXPECT().NativeRingingEnabled().Return(false).AnyTimes()
		m.EXPECT().ActiveCall(gomock.Any()).Return(&calls.Call{PostID: callPostID, CallID: model.NewId()}, nil).AnyTimes()
	})
	result, err := e.svc.Start(e.request("alice"))
	require.NoError(t, err)
	assert.True(t, result.CallActive)
	assert.Equal(t, callPostID, e.api.eventsFor(ring.EventIncomingRing)[0].Payload["call_post_id"])

	e.scheduler.runAll()
	require.Len(t, e.api.posts, 1)
	assert.Contains(t, e.api.posts[0].Message, "https://chat.example.com/acme/pl/"+callPostID)
}

func TestRespond(t *testing.T) {
	e := newEnv(t)
	result, err := e.svc.Start(e.request("alice", "bob"))
	require.NoError(t, err)

	t.Run("decline stops other sessions and tells the caller", func(t *testing.T) {
		require.NoError(t, e.svc.Respond(result.RingID, e.alice.Id, ring.ActionDecline))

		stops := e.api.eventsFor(ring.EventRingStop)
		require.Len(t, stops, 1)
		assert.Equal(t, e.alice.Id, stops[0].UserID)
		assert.Equal(t, "decline", stops[0].Payload["reason"])

		require.Len(t, e.api.ephemerals, 1)
		assert.Equal(t, e.caller.Id, e.api.ephemerals[0].GetProp("target_user_id"))
		assert.Equal(t, e.channel.Id, e.api.ephemerals[0].ChannelId)
		assert.True(t, strings.HasPrefix(e.api.ephemerals[0].Message, "ring.declined"))
	})

	t.Run("responses are idempotent", func(t *testing.T) {
		require.NoError(t, e.svc.Respond(result.RingID, e.alice.Id, ring.ActionDecline))
		require.NoError(t, e.svc.Respond(result.RingID, e.alice.Id, ring.ActionAccept))
		assert.Len(t, e.api.eventsFor(ring.EventRingStop), 1)
		assert.Len(t, e.api.ephemerals, 1)
	})

	t.Run("accept does not notify the caller", func(t *testing.T) {
		require.NoError(t, e.svc.Respond(result.RingID, e.bob.Id, ring.ActionAccept))
		assert.Len(t, e.api.ephemerals, 1)
		assert.Len(t, e.api.eventsFor(ring.EventRingStop), 2)
	})

	t.Run("only recipients can respond", func(t *testing.T) {
		requireCode(t, e.svc.Respond(result.RingID, e.carol.Id, ring.ActionAccept), ring.ErrNotRecipient)
	})

	t.Run("unknown ring", func(t *testing.T) {
		requireCode(t, e.svc.Respond(model.NewId(), e.alice.Id, ring.ActionAccept), ring.ErrRingNotFound)
	})

	t.Run("answered recipients get no fallback message", func(t *testing.T) {
		e.scheduler.runAll()
		assert.Empty(t, e.api.posts)
	})
}

func TestCancel(t *testing.T) {
	e := newEnv(t)
	result, err := e.svc.Start(e.request("alice", "bob"))
	require.NoError(t, err)
	require.NoError(t, e.svc.Respond(result.RingID, e.alice.Id, ring.ActionDecline))

	t.Run("only the caller can cancel", func(t *testing.T) {
		_, err := e.svc.Cancel(e.alice.Id, result.RingID)
		requireCode(t, err, ring.ErrNotCaller)
	})

	t.Run("cancels the latest ring and stops pending recipients", func(t *testing.T) {
		stopped, err := e.svc.Cancel(e.caller.Id, "")
		require.NoError(t, err)
		assert.Equal(t, 1, stopped)
		var cancelled []string
		for _, ev := range e.api.eventsFor(ring.EventRingStop) {
			if ev.Payload["reason"] == "cancelled" {
				cancelled = append(cancelled, ev.UserID)
			}
		}
		assert.Equal(t, []string{e.bob.Id}, cancelled)
	})

	t.Run("cancel twice", func(t *testing.T) {
		stopped, err := e.svc.Cancel(e.caller.Id, result.RingID)
		require.NoError(t, err)
		assert.Zero(t, stopped)
	})

	t.Run("no ring", func(t *testing.T) {
		_, err := e.svc.Cancel(e.carol.Id, "")
		requireCode(t, err, ring.ErrRingNotFound)
	})
}

func TestSetCallID(t *testing.T) {
	callID := model.NewId()
	e := newEnvWithCalls(t, func(m *mocks.MockClient) {
		m.EXPECT().Available().Return(nil).AnyTimes()
		m.EXPECT().TestMode().Return(false).AnyTimes()
		m.EXPECT().NativeRingingEnabled().Return(false).AnyTimes()
		m.EXPECT().ActiveCall(gomock.Any()).Return(nil, nil).AnyTimes()
		m.EXPECT().SetCallID(gomock.Any(), callID).Return(nil).Times(1)
	})
	result, err := e.svc.Start(e.request("alice", "bob"))
	require.NoError(t, err)
	require.NoError(t, e.svc.Respond(result.RingID, e.alice.Id, ring.ActionDecline))

	requireCode(t, e.svc.SetCallID(result.RingID, e.alice.Id, callID), ring.ErrNotCaller)
	requireCode(t, e.svc.SetCallID(result.RingID, e.caller.Id, "bad"), ring.ErrInvalidRequest)

	require.NoError(t, e.svc.SetCallID(result.RingID, e.caller.Id, callID))
	updates := e.api.eventsFor(ring.EventRingUpdated)
	require.Len(t, updates, 1, "only recipients still ringing get the update")
	assert.Equal(t, e.bob.Id, updates[0].UserID)
	assert.Equal(t, callID, updates[0].Payload["call_id"])

	// Reporting the same call again is a no-op (SetCallID on the Calls client is expected once).
	require.NoError(t, e.svc.SetCallID(result.RingID, e.caller.Id, callID))
}

func TestCallsIntegration(t *testing.T) {
	t.Run("calls plugin unavailable", func(t *testing.T) {
		e := newEnvWithCalls(t, func(m *mocks.MockClient) {
			m.EXPECT().Available().Return(calls.ErrNotRunning)
		})
		_, err := e.svc.Start(e.request("alice"))
		requireCode(t, err, ring.ErrCallsUnavailable)
	})

	t.Run("calls outside DMs not allowed by the Calls plugin", func(t *testing.T) {
		e := newEnv(t)
		notAllowed, allowed := false, true

		req := e.request("alice")
		req.GroupCallsAllowedHint = &notAllowed
		_, err := e.svc.Start(req)
		requireCode(t, err, ring.ErrGroupCallsBlocked)
		assert.Empty(t, e.api.events)

		req.GroupCallsAllowedHint = &allowed
		_, err = e.svc.Start(req)
		require.NoError(t, err)

		dm := e.api.addChannel("", model.GetDMNameFromIds(e.caller.Id, e.alice.Id), model.ChannelTypeDirect, e.caller, e.alice)
		req = e.request("alice")
		req.ChannelID = dm.Id
		req.GroupCallsAllowedHint = &notAllowed
		_, err = e.svc.Start(req)
		require.NoError(t, err, "DM calls are always allowed")
	})

	t.Run("test mode blocks non-admins from starting a call", func(t *testing.T) {
		e := newEnvWithCalls(t, func(m *mocks.MockClient) {
			m.EXPECT().Available().Return(nil).AnyTimes()
			m.EXPECT().ActiveCall(gomock.Any()).Return(nil, nil).AnyTimes()
			m.EXPECT().TestMode().Return(true).AnyTimes()
			m.EXPECT().NativeRingingEnabled().Return(false).AnyTimes()
		})
		_, err := e.svc.Start(e.request("alice"))
		requireCode(t, err, ring.ErrCallsTestMode)

		req := e.request("alice")
		req.CallActiveHint = true
		_, err = e.svc.Start(req)
		require.NoError(t, err, "joining a running call is allowed in test mode")
	})

	gmSetup := func(t *testing.T, active *calls.Call) (*env, *model.Channel) {
		e := newEnvWithCalls(t, func(m *mocks.MockClient) {
			m.EXPECT().Available().Return(nil).AnyTimes()
			m.EXPECT().TestMode().Return(false).AnyTimes()
			m.EXPECT().NativeRingingEnabled().Return(true).AnyTimes()
			m.EXPECT().ActiveCall(gomock.Any()).Return(active, nil).AnyTimes()
		})
		gm := e.api.addChannel("", "gm-name", model.ChannelTypeGroup, e.caller, e.alice, e.bob)
		return e, gm
	}

	t.Run("new call in a GM defers to native ringing", func(t *testing.T) {
		e, gm := gmSetup(t, nil)
		req := e.request()
		req.ChannelID = gm.Id
		req.WholeChannel = true
		result, err := e.svc.Start(req)
		require.NoError(t, err)
		assert.True(t, result.DeferredToNative)
		assert.Empty(t, result.RingID)
		assert.Empty(t, e.api.events)
	})

	t.Run("running call in a GM is rung by the plugin", func(t *testing.T) {
		e, gm := gmSetup(t, &calls.Call{ChannelID: "x", PostID: model.NewId()})
		req := e.request()
		req.ChannelID = gm.Id
		req.WholeChannel = true
		result, err := e.svc.Start(req)
		require.NoError(t, err)
		assert.False(t, result.DeferredToNative)
		assert.Equal(t, sortedIDs(e.alice, e.bob), e.api.recipientsOf(ring.EventIncomingRing))
	})

	t.Run("deferral can be turned off", func(t *testing.T) {
		e, gm := gmSetup(t, nil)
		e.cfg.DeferToNativeRinging = false
		req := e.request()
		req.ChannelID = gm.Id
		req.WholeChannel = true
		result, err := e.svc.Start(req)
		require.NoError(t, err)
		assert.False(t, result.DeferredToNative)
		assert.Len(t, e.api.eventsFor(ring.EventIncomingRing), 2)
	})

	t.Run("DM fallback link uses the recipient's team", func(t *testing.T) {
		e, gm := gmSetup(t, nil)
		e.cfg.DeferToNativeRinging = false
		e.api.statuses[e.alice.Id] = model.StatusOffline
		req := e.request("alice")
		req.ChannelID = gm.Id
		_, err := e.svc.Start(req)
		require.NoError(t, err)
		e.scheduler.runAll()
		require.NotEmpty(t, e.api.posts)
		assert.Contains(t, e.api.posts[0].Message, "https://chat.example.com/acme/messages/gm-name")
	})
}
