package ring_test

import (
	"bytes"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/utaku/mattermost-plugin-group-ring/server/ring"
)

// fakeKV is an in-memory plugin KV store with compare-and-set semantics. Expiry is ignored.
type fakeKV struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newFakeKV() *fakeKV {
	return &fakeKV{data: map[string][]byte{}}
}

func (kv *fakeKV) KVSetWithOptions(key string, value []byte, options model.PluginKVSetOptions) (bool, *model.AppError) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	if options.Atomic {
		current, exists := kv.data[key]
		if options.OldValue == nil {
			if exists {
				return false, nil
			}
		} else if !exists || !bytes.Equal(current, options.OldValue) {
			return false, nil
		}
	}
	if value == nil {
		delete(kv.data, key)
		return true, nil
	}
	kv.data[key] = append([]byte(nil), value...)
	return true, nil
}

func (kv *fakeKV) KVGet(key string) ([]byte, *model.AppError) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	if v, ok := kv.data[key]; ok {
		return append([]byte(nil), v...), nil
	}
	return nil, nil
}

type publishedEvent struct {
	Event   string
	Payload map[string]any
	UserID  string
}

type logLine struct {
	Level string
	Msg   string
	KV    map[string]any
}

// fakeAPI is an in-memory implementation of ring.API.
type fakeAPI struct {
	mu sync.Mutex

	users       map[string]*model.User
	channels    map[string]*model.Channel
	teams       map[string]*model.Team
	members     map[string]map[string]*model.ChannelMember // channel -> user -> member
	statuses    map[string]string
	canPost     map[string]bool // "channel:user" -> allowed; default true
	sysAdmins   map[string]bool
	teamAdmins  map[string]bool // "team:user"
	usersByName map[string]*model.User

	events     []publishedEvent
	ephemerals []*model.Post
	posts      []*model.Post
	logs       []logLine
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		users:       map[string]*model.User{},
		channels:    map[string]*model.Channel{},
		teams:       map[string]*model.Team{},
		members:     map[string]map[string]*model.ChannelMember{},
		statuses:    map[string]string{},
		canPost:     map[string]bool{},
		sysAdmins:   map[string]bool{},
		teamAdmins:  map[string]bool{},
		usersByName: map[string]*model.User{},
	}
}

func notFound(where string) *model.AppError {
	return model.NewAppError(where, "not_found", nil, "", http.StatusNotFound)
}

func (f *fakeAPI) addUser(username string, opts ...func(*model.User)) *model.User {
	u := &model.User{Id: model.NewId(), Username: username, Locale: "en"}
	for _, opt := range opts {
		opt(u)
	}
	f.users[u.Id] = u
	f.usersByName[username] = u
	f.statuses[u.Id] = model.StatusOnline
	return u
}

func asBot(u *model.User)       { u.IsBot = true }
func deactivated(u *model.User) { u.DeleteAt = 1 }

func (f *fakeAPI) addTeam(name string) *model.Team {
	t := &model.Team{Id: model.NewId(), Name: name}
	f.teams[t.Id] = t
	return t
}

func (f *fakeAPI) addChannel(teamID, name string, channelType model.ChannelType, members ...*model.User) *model.Channel {
	c := &model.Channel{Id: model.NewId(), TeamId: teamID, Name: name, DisplayName: "Display " + name, Type: channelType}
	f.channels[c.Id] = c
	f.members[c.Id] = map[string]*model.ChannelMember{}
	for _, u := range members {
		f.join(c, u)
	}
	return c
}

func (f *fakeAPI) join(c *model.Channel, u *model.User) {
	f.members[c.Id][u.Id] = &model.ChannelMember{ChannelId: c.Id, UserId: u.Id}
}

func (f *fakeAPI) eventsFor(event string) []publishedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []publishedEvent
	for _, e := range f.events {
		if e.Event == event {
			out = append(out, e)
		}
	}
	return out
}

func (f *fakeAPI) recipientsOf(event string) []string {
	var ids []string
	for _, e := range f.eventsFor(event) {
		ids = append(ids, e.UserID)
	}
	sort.Strings(ids)
	return ids
}

func (f *fakeAPI) dmRecipients(botID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for _, p := range f.posts {
		c := f.channels[p.ChannelId]
		if c == nil || c.Type != model.ChannelTypeDirect {
			continue
		}
		ids = append(ids, c.GetOtherUserIdForDM(botID))
	}
	sort.Strings(ids)
	return ids
}

func (f *fakeAPI) GetChannel(channelID string) (*model.Channel, *model.AppError) {
	if c, ok := f.channels[channelID]; ok {
		return c, nil
	}
	return nil, notFound("GetChannel")
}

func (f *fakeAPI) GetChannelByName(teamID, name string, _ bool) (*model.Channel, *model.AppError) {
	for _, c := range f.channels {
		if c.TeamId == teamID && c.Name == name {
			return c, nil
		}
	}
	return nil, notFound("GetChannelByName")
}

func (f *fakeAPI) GetChannelMember(channelID, userID string) (*model.ChannelMember, *model.AppError) {
	if m, ok := f.members[channelID][userID]; ok {
		return m, nil
	}
	return nil, notFound("GetChannelMember")
}

func (f *fakeAPI) GetUsersInChannel(channelID, _ string, page, perPage int) ([]*model.User, *model.AppError) {
	var ids []string
	for id := range f.members[channelID] {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	start := page * perPage
	if start >= len(ids) {
		return []*model.User{}, nil
	}
	end := min(start+perPage, len(ids))
	users := make([]*model.User, 0, end-start)
	for _, id := range ids[start:end] {
		users = append(users, f.users[id])
	}
	return users, nil
}

func (f *fakeAPI) GetUser(userID string) (*model.User, *model.AppError) {
	if u, ok := f.users[userID]; ok {
		return u, nil
	}
	return nil, notFound("GetUser")
}

func (f *fakeAPI) GetUserByUsername(name string) (*model.User, *model.AppError) {
	if u, ok := f.usersByName[name]; ok {
		return u, nil
	}
	return nil, notFound("GetUserByUsername")
}

func (f *fakeAPI) GetUserStatusesByIds(userIDs []string) ([]*model.Status, *model.AppError) {
	statuses := make([]*model.Status, 0, len(userIDs))
	for _, id := range userIDs {
		if st, ok := f.statuses[id]; ok {
			statuses = append(statuses, &model.Status{UserId: id, Status: st})
		}
	}
	return statuses, nil
}

func (f *fakeAPI) GetTeam(teamID string) (*model.Team, *model.AppError) {
	if t, ok := f.teams[teamID]; ok {
		return t, nil
	}
	return nil, notFound("GetTeam")
}

func (f *fakeAPI) GetTeamsForUser(_ string) ([]*model.Team, *model.AppError) {
	var teams []*model.Team
	for _, t := range f.teams {
		teams = append(teams, t)
	}
	sort.Slice(teams, func(i, j int) bool { return teams[i].Name < teams[j].Name })
	return teams, nil
}

func (f *fakeAPI) GetDirectChannel(userID1, userID2 string) (*model.Channel, *model.AppError) {
	name := model.GetDMNameFromIds(userID1, userID2)
	for _, c := range f.channels {
		if c.Name == name {
			return c, nil
		}
	}
	c := &model.Channel{Id: model.NewId(), Name: name, Type: model.ChannelTypeDirect}
	f.channels[c.Id] = c
	return c, nil
}

func (f *fakeAPI) HasPermissionTo(userID string, permission *model.Permission) bool {
	return permission.Id == model.PermissionManageSystem.Id && f.sysAdmins[userID]
}

func (f *fakeAPI) HasPermissionToTeam(userID, teamID string, permission *model.Permission) bool {
	return permission.Id == model.PermissionManageTeam.Id && f.teamAdmins[teamID+":"+userID]
}

func (f *fakeAPI) HasPermissionToChannel(userID, channelID string, permission *model.Permission) bool {
	if permission.Id != model.PermissionCreatePost.Id {
		return false
	}
	if _, member := f.members[channelID][userID]; !member {
		return false
	}
	if allowed, ok := f.canPost[channelID+":"+userID]; ok {
		return allowed
	}
	return true
}

func (f *fakeAPI) PublishWebSocketEvent(event string, payload map[string]any, broadcast *model.WebsocketBroadcast) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, publishedEvent{Event: event, Payload: payload, UserID: broadcast.UserId})
}

func (f *fakeAPI) SendEphemeralPost(userID string, post *model.Post) *model.Post {
	f.mu.Lock()
	defer f.mu.Unlock()
	post.AddProp("target_user_id", userID)
	f.ephemerals = append(f.ephemerals, post)
	return post
}

func (f *fakeAPI) CreatePost(post *model.Post) (*model.Post, *model.AppError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	post.Id = model.NewId()
	f.posts = append(f.posts, post)
	return post, nil
}

func (f *fakeAPI) GetConfig() *model.Config {
	cfg := &model.Config{}
	cfg.SetDefaults()
	return cfg
}

func (f *fakeAPI) log(level, msg string, kv []any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fields := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok {
			fields[k] = kv[i+1]
		}
	}
	f.logs = append(f.logs, logLine{Level: level, Msg: msg, KV: fields})
}

func (f *fakeAPI) LogDebug(msg string, kv ...any) { f.log("debug", msg, kv) }
func (f *fakeAPI) LogInfo(msg string, kv ...any)  { f.log("info", msg, kv) }
func (f *fakeAPI) LogWarn(msg string, kv ...any)  { f.log("warn", msg, kv) }
func (f *fakeAPI) LogError(msg string, kv ...any) { f.log("error", msg, kv) }

// fakeScheduler records scheduled functions so tests decide when they run.
type fakeScheduler struct {
	mu    sync.Mutex
	tasks []scheduledTask
}

type scheduledTask struct {
	delay time.Duration
	fn    func()
}

func (s *fakeScheduler) AfterFunc(d time.Duration, f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, scheduledTask{delay: d, fn: f})
}

// runAll runs every scheduled task, including tasks scheduled while running.
func (s *fakeScheduler) runAll() {
	for {
		s.mu.Lock()
		if len(s.tasks) == 0 {
			s.mu.Unlock()
			return
		}
		task := s.tasks[0]
		s.tasks = s.tasks[1:]
		s.mu.Unlock()
		task.fn()
	}
}

// fakeClock is a controllable clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

var _ ring.API = (*fakeAPI)(nil)
