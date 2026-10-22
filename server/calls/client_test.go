package calls

import (
	"net/http"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAPI struct {
	status   *model.PluginStatus
	settings map[string]any
	kv       map[string][]byte
}

func (f *fakeAPI) GetPluginStatus(string) (*model.PluginStatus, *model.AppError) {
	if f.status == nil {
		return nil, model.NewAppError("GetPluginStatus", "not_found", nil, "", http.StatusNotFound)
	}
	return f.status, nil
}

func (f *fakeAPI) GetConfig() *model.Config {
	cfg := &model.Config{}
	cfg.SetDefaults()
	if f.settings != nil {
		cfg.PluginSettings.Plugins[PluginID] = f.settings
	}
	return cfg
}

func (f *fakeAPI) KVSetWithOptions(key string, value []byte, _ model.PluginKVSetOptions) (bool, *model.AppError) {
	f.kv[key] = value
	return true, nil
}

func (f *fakeAPI) KVGet(key string) ([]byte, *model.AppError) { return f.kv[key], nil }

func (f *fakeAPI) KVDelete(key string) *model.AppError {
	delete(f.kv, key)
	return nil
}

func (f *fakeAPI) LogDebug(string, ...any) {}
func (f *fakeAPI) LogWarn(string, ...any)  {}

func newFake() *fakeAPI {
	return &fakeAPI{kv: map[string][]byte{}}
}

func TestAvailability(t *testing.T) {
	api := newFake()
	c := NewClient(api)
	assert.ErrorIs(t, c.Available(), ErrNotInstalled)

	api.status = &model.PluginStatus{State: model.PluginStateFailedToStart, Version: "1.12.6"}
	assert.ErrorIs(t, c.Available(), ErrNotRunning)

	api.status.State = model.PluginStateRunning
	assert.NoError(t, c.Available())
	assert.Equal(t, "1.12.6", c.Version())
}

func TestSettings(t *testing.T) {
	api := newFake()
	c := NewClient(api)
	assert.False(t, c.NativeRingingEnabled(), "ringing is off by default in Calls")
	assert.False(t, c.TestMode(), "test mode is only reported when explicitly set")

	api.settings = map[string]any{"enableringing": true, "defaultenabled": false}
	assert.True(t, c.NativeRingingEnabled())
	assert.True(t, c.TestMode())

	api.settings = map[string]any{"EnableRinging": "true", "DefaultEnabled": true}
	assert.True(t, c.NativeRingingEnabled())
	assert.False(t, c.TestMode())
}

func TestCallTracking(t *testing.T) {
	api := newFake()
	c := NewClient(api)
	channelID := model.NewId()

	// Ordinary posts are ignored.
	c.ObservePost(&model.Post{Id: model.NewId(), ChannelId: channelID, Type: ""})
	call, err := c.ActiveCall(channelID)
	require.NoError(t, err)
	assert.Nil(t, call)

	// The call ID reported by the webapp may arrive before the post.
	callID := model.NewId()
	require.NoError(t, c.SetCallID(channelID, callID))

	post := &model.Post{Id: model.NewId(), ChannelId: channelID, Type: CallPostType}
	post.AddProp("start_at", float64(1700000000000))
	c.ObservePost(post)

	call, err = c.ActiveCall(channelID)
	require.NoError(t, err)
	require.NotNil(t, call)
	assert.Equal(t, post.Id, call.PostID)
	assert.Equal(t, post.Id, call.ThreadID)
	assert.Equal(t, int64(1700000000000), call.StartAt)
	assert.Equal(t, callID, call.CallID)

	// The end of an older call does not clear a newer one.
	old := &model.Post{Id: model.NewId(), ChannelId: channelID, Type: CallPostType}
	old.AddProp("end_at", int64(1))
	c.ObservePost(old)
	call, _ = c.ActiveCall(channelID)
	assert.NotNil(t, call)

	ended := post.Clone()
	ended.AddProp("end_at", int64(1700000060000))
	c.ObservePost(ended)
	call, err = c.ActiveCall(channelID)
	require.NoError(t, err)
	assert.Nil(t, call)

	assert.Error(t, c.SetCallID(channelID, "not-an-id"))
}
