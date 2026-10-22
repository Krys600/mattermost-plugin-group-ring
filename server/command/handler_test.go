package command

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/utaku/mattermost-plugin-group-ring/server/i18n"
	"github.com/utaku/mattermost-plugin-group-ring/server/ring"
)

type fakeService struct {
	lastRequest ring.Request
	result      *ring.Result
	err         error
	cancelled   int
}

func (f *fakeService) Start(req ring.Request) (*ring.Result, error) {
	f.lastRequest = req
	return f.result, f.err
}

func (f *fakeService) Cancel(_, _ string) (int, error) {
	return f.cancelled, f.err
}

func newHandler(t *testing.T, svc Service) *Handler {
	t.Helper()
	bundle, err := i18n.Load()
	require.NoError(t, err)
	return NewHandler(svc, bundle.T)
}

func TestRunHelp(t *testing.T) {
	out := newHandler(t, &fakeService{}).Run(Input{Text: "/ring --help"})
	assert.Equal(t, KindHelp, out.Kind)
	assert.Contains(t, out.Text, "/ring @channel")
}

func TestRunParseError(t *testing.T) {
	out := newHandler(t, &fakeService{}).Run(Input{Text: "/ring alice"})
	assert.Equal(t, KindError, out.Kind)
	assert.Equal(t, 400, out.Status)
	assert.Contains(t, out.Text, "Unexpected argument `alice`")
	assert.Contains(t, out.Text, "Usage:")
}

func TestRunBuildsRequest(t *testing.T) {
	svc := &fakeService{result: &ring.Result{
		Channel:        &model.Channel{Name: "town-square"},
		RecipientCount: 2,
		Rung:           []string{"alice"},
		DMOnly:         []string{"bob"},
		Skipped:        []ring.Skipped{{Username: "robot", Reason: ring.SkipBot}},
	}}
	out := newHandler(t, svc).Run(Input{UserID: "u", ChannelID: "c", TeamID: "t", Text: "/ring @alice @bob @robot", Source: "command", CallActiveHint: true})

	assert.Equal(t, ring.Request{
		CallerID: "u", ChannelID: "c", TeamID: "t",
		Usernames: []string{"alice", "bob", "robot"}, CallActiveHint: true, Source: "command",
	}, svc.lastRequest)
	assert.Equal(t, KindRinging, out.Kind)
	assert.Contains(t, out.Text, "Ringing 1 people in ~town-square: @alice.")
	assert.Contains(t, out.Text, "Sent a direct message with a link to the call: @bob.")
	assert.Contains(t, out.Text, "Not rung: @robot (bot).")
}

func TestRunNeedsConfirmation(t *testing.T) {
	svc := &fakeService{result: &ring.Result{NeedsConfirmation: true, RecipientCount: 42}}
	out := newHandler(t, svc).Run(Input{Text: "/ring @channel"})
	assert.Equal(t, KindNeedsConfirmation, out.Kind)
	assert.Contains(t, out.Text, "This will ring 42 people")
	assert.Contains(t, out.Text, "`/ring @channel --confirm`")

	newHandler(t, svc).Run(Input{Text: "/ring @channel --confirm"})
	assert.True(t, svc.lastRequest.Confirmed)
}

func TestRunErrors(t *testing.T) {
	svc := &fakeService{err: &ring.Error{Code: ring.ErrRateLimited, Params: map[string]any{"Seconds": 12}}}
	out := newHandler(t, svc).Run(Input{Text: "/ring @alice"})
	assert.Equal(t, KindError, out.Kind)
	assert.Equal(t, 429, out.Status)
	assert.Equal(t, "rate_limited", out.ErrorCode)
	assert.Equal(t, "You are ringing too often. Try again in 12 seconds.", out.Text)

	svc.err = &ring.Error{Code: ring.ErrNoRecipients, Skipped: []ring.Skipped{{Username: "outside", Reason: ring.SkipNotMember}}}
	out = newHandler(t, svc).Run(Input{Text: "/ring @outside"})
	assert.Contains(t, out.Text, "@outside (not a member of this channel)")

	svc.err = fmt.Errorf("boom")
	out = newHandler(t, svc).Run(Input{Text: "/ring @alice"})
	assert.Equal(t, 500, out.Status)
	assert.NotContains(t, out.Text, "boom", "internal details are not shown to users")
}

func TestRunCancel(t *testing.T) {
	out := newHandler(t, &fakeService{cancelled: 3}).Run(Input{Text: "/ring --cancel"})
	assert.Equal(t, KindCancelled, out.Kind)
	assert.Equal(t, "Stopped ringing 3 people.", out.Text)
}

func TestLongListsAreSummarized(t *testing.T) {
	names := make([]string, 25)
	for i := range names {
		names[i] = fmt.Sprintf("user%d", i)
	}
	svc := &fakeService{result: &ring.Result{Rung: names, RecipientCount: 25}}
	out := newHandler(t, svc).Run(Input{Text: "/ring @channel"})
	assert.Contains(t, out.Text, "and 15 more")
	assert.Equal(t, 10, strings.Count(strings.Split(out.Text, "\n")[0], "@user"))
}

func TestCatalogHasEveryErrorCode(t *testing.T) {
	bundle, err := i18n.Load()
	require.NoError(t, err)
	codes := []ring.ErrorCode{
		ring.ErrDisabled, ring.ErrChannelNotFound, ring.ErrChannelType, ring.ErrNotChannelMember,
		ring.ErrNoPostPermission, ring.ErrRoleNotAllowed, ring.ErrCallsUnavailable, ring.ErrCallsTestMode, ring.ErrGroupCallsBlocked,
		ring.ErrNoRecipients, ring.ErrTooManyRecipients, ring.ErrRateLimited, ring.ErrRingNotFound,
		ring.ErrNotRecipient, ring.ErrNotCaller, ring.ErrInvalidRequest, ring.ErrMixedTargets, ring.ErrInternal,
	}
	for _, code := range codes {
		id := "ring.error." + string(code)
		assert.NotEqual(t, id, bundle.T("en", id, nil), "missing translation for %s", id)
	}
	for _, reason := range []ring.SkipReason{ring.SkipNotMember, ring.SkipNotFound, ring.SkipBot, ring.SkipInactive, ring.SkipDND} {
		id := "ring.skip." + string(reason)
		assert.NotEqual(t, id, bundle.T("en", id, nil), "missing translation for %s", id)
	}
}

func TestRunServerCommandAddsStartCallHint(t *testing.T) {
	svc := &fakeService{result: &ring.Result{Rung: []string{"alice"}, RecipientCount: 1}}
	h := newHandler(t, svc)

	out := h.Run(Input{Text: "/ring @alice", Source: SourceServerCommand})
	assert.Contains(t, out.Text, "Start or join the call")

	out = h.Run(Input{Text: "/ring @alice", Source: "webapp_command"})
	assert.NotContains(t, out.Text, "Start or join the call", "the webapp starts the call itself")
}
