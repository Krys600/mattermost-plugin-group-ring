// Package ring implements the ring service: permission checks, recipient resolution and
// filtering, limits, real-time delivery to recipients, fallback direct messages and the
// lifecycle of recipient responses.
package ring

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	// RecordRetention keeps a ring record around after it timed out, so late responses
	// (for example a "missed" report from a slow client) still find it.
	RecordRetention = 2 * time.Minute

	// channelMembersPerPage is the page size used when listing channel members.
	channelMembersPerPage = 200

	rateLimitWindow = time.Minute
)

// errNoChange aborts a ModifyRing call without side effects.
var errNoChange = errors.New("no change")

// Service is the ring service. It is safe for concurrent use.
type Service struct {
	d Dependencies
}

// NewService creates a ring service, filling in production defaults for optional dependencies.
func NewService(d Dependencies) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.AfterFunc == nil {
		d.AfterFunc = func(delay time.Duration, f func()) { time.AfterFunc(delay, f) }
	}
	if d.NewID == nil {
		d.NewID = model.NewId
	}
	if d.Translate == nil {
		d.Translate = func(_, id string, _ map[string]any) string { return id }
	}
	return &Service{d: d}
}

// Start validates a ring request, resolves and filters its recipients and rings them.
func (s *Service) Start(req Request) (*Result, error) {
	cfg := s.d.Config()
	if !cfg.Enabled {
		return nil, newError(ErrDisabled, nil)
	}

	hasUsers := len(req.Usernames) > 0 || len(req.UserIDs) > 0
	hasChannel := req.WholeChannel || req.TargetChannelName != ""
	if hasUsers && hasChannel {
		return nil, newError(ErrMixedTargets, nil)
	}
	if !hasUsers && !hasChannel {
		return nil, newError(ErrInvalidRequest, nil)
	}

	channel, err := s.resolveChannel(req)
	if err != nil {
		return nil, err
	}
	if !cfg.ChannelTypes.Allows(channel.Type) {
		return nil, newError(ErrChannelType, nil)
	}
	if err = s.checkCaller(req.CallerID, channel, cfg); err != nil {
		return nil, err
	}

	if callsErr := s.d.Calls.Available(); callsErr != nil {
		return nil, &Error{Code: ErrCallsUnavailable, cause: callsErr}
	}
	if channel.Type != model.ChannelTypeDirect && req.GroupCallsAllowedHint != nil && !*req.GroupCallsAllowedHint {
		// Calls would refuse to start or join the call, so do not ring anyone into it.
		return nil, newError(ErrGroupCallsBlocked, nil)
	}

	activeCall, err := s.d.Calls.ActiveCall(channel.Id)
	if err != nil {
		s.d.API.LogWarn("Failed to read active call", "channel_id", channel.Id, "err", err.Error())
	}
	callActive := activeCall != nil || req.CallActiveHint
	if !callActive && s.d.Calls.TestMode() && !s.d.API.HasPermissionTo(req.CallerID, model.PermissionManageSystem) {
		return nil, newError(ErrCallsTestMode, nil)
	}

	candidates, skipped, err := s.resolveRecipients(req, channel, cfg)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, &Error{Code: ErrNoRecipients, Skipped: skipped}
	}
	if len(candidates) > cfg.MaxRecipients {
		return nil, newError(ErrTooManyRecipients, map[string]any{"Max": cfg.MaxRecipients})
	}

	result := &Result{
		Channel:        channel,
		ChannelID:      channel.Id,
		ChannelName:    channel.Name,
		TeamID:         channel.TeamId,
		CallActive:     callActive,
		RecipientCount: len(candidates),
		Rung:           []string{},
		Silent:         []string{},
		DMOnly:         []string{},
		TimeoutSeconds: int(cfg.RingTimeout.Seconds()),
	}

	if cfg.ConfirmThreshold > 0 && len(candidates) > cfg.ConfirmThreshold && !req.Confirmed {
		result.NeedsConfirmation = true
		result.Skipped = skipped
		return result, nil
	}

	recipients, dndSkipped := s.classify(candidates, cfg)
	skipped = append(skipped, dndSkipped...)
	result.Skipped = skipped
	if len(recipients) == 0 {
		return nil, &Error{Code: ErrNoRecipients, Skipped: skipped}
	}
	result.RecipientCount = len(recipients)

	if cfg.RateLimitPerMinute > 0 {
		allowed, retryAfter, rlErr := s.d.Store.TakeRateLimitToken(req.CallerID, cfg.RateLimitPerMinute, rateLimitWindow, s.d.Now())
		if rlErr != nil {
			return nil, internalError(rlErr)
		}
		if !allowed {
			return nil, newError(ErrRateLimited, map[string]any{"Seconds": int(math.Ceil(retryAfter.Seconds()))})
		}
	}

	for _, u := range candidates {
		switch recipients[u.Id] {
		case DeliveryRing:
			result.Rung = append(result.Rung, u.Username)
		case DeliverySilent:
			result.Silent = append(result.Silent, u.Username)
		case DeliveryDM:
			result.DMOnly = append(result.DMOnly, u.Username)
		}
	}

	if cfg.DeferToNativeRinging && isDirectOrGroup(channel) && !callActive && s.d.Calls.NativeRingingEnabled() {
		result.DeferredToNative = true
		s.auditStart(req, channel, result, "")
		return result, nil
	}

	now := s.d.Now()
	r := &Ring{
		ID:         s.d.NewID(),
		CallerID:   req.CallerID,
		ChannelID:  channel.Id,
		TeamID:     channel.TeamId,
		CreatedAt:  now.UnixMilli(),
		ExpiresAt:  now.Add(cfg.RingTimeout).UnixMilli(),
		Recipients: make(map[string]*Recipient, len(recipients)),
	}
	if activeCall != nil {
		r.CallID = activeCall.CallID
		r.CallPostID = activeCall.PostID
	}
	for userID, mode := range recipients {
		r.Recipients[userID] = &Recipient{Mode: mode, Status: StatusPending}
	}

	if err = s.d.Store.SaveRing(r); err != nil {
		return nil, internalError(err)
	}
	if err = s.d.Store.SetLatestRing(req.CallerID, r.ID, cfg.RingTimeout+RecordRetention); err != nil {
		s.d.API.LogWarn("Failed to remember latest ring", "ring_id", r.ID, "err", err.Error())
	}
	result.RingID = r.ID

	caller, appErr := s.d.API.GetUser(req.CallerID)
	if appErr != nil {
		return nil, internalError(appErr)
	}

	inApp := 0
	dmOnly := make([]string, 0)
	for userID, rec := range r.Recipients {
		if rec.Mode == DeliveryDM {
			dmOnly = append(dmOnly, userID)
			continue
		}
		inApp++
		s.d.API.PublishWebSocketEvent(EventIncomingRing, s.incomingPayload(r, rec, caller, channel, cfg), &model.WebsocketBroadcast{UserId: userID})
	}

	if len(dmOnly) > 0 {
		ringID := r.ID
		s.d.AfterFunc(0, func() { s.sendDirectMessages(ringID, dmOnly) })
	}
	if inApp > 0 {
		ringID := r.ID
		s.d.AfterFunc(cfg.FallbackDelay, func() { s.SendFallbackMessages(ringID) })
	}

	s.auditStart(req, channel, result, r.ID)
	return result, nil
}

func (s *Service) auditStart(req Request, channel *model.Channel, result *Result, ringID string) {
	s.d.API.LogInfo("Ring started",
		"ring_id", ringID,
		"caller_id", req.CallerID,
		"channel_id", channel.Id,
		"channel_type", string(channel.Type),
		"recipient_count", result.RecipientCount,
		"rung", len(result.Rung),
		"silent", len(result.Silent),
		"dm_only", len(result.DMOnly),
		"skipped", len(result.Skipped),
		"deferred_to_native", result.DeferredToNative,
		"source", req.Source,
	)
}

func (s *Service) resolveChannel(req Request) (*model.Channel, error) {
	var (
		channel *model.Channel
		appErr  *model.AppError
	)
	if req.TargetChannelName != "" {
		name := strings.ToLower(strings.TrimPrefix(req.TargetChannelName, "~"))
		if req.TeamID == "" {
			return nil, newError(ErrChannelNotFound, map[string]any{"Name": name})
		}
		channel, appErr = s.d.API.GetChannelByName(req.TeamID, name, false)
		if appErr != nil || channel == nil {
			return nil, newError(ErrChannelNotFound, map[string]any{"Name": name})
		}
	} else {
		channel, appErr = s.d.API.GetChannel(req.ChannelID)
		if appErr != nil || channel == nil {
			return nil, newError(ErrChannelNotFound, map[string]any{"Name": req.ChannelID})
		}
	}
	if channel.DeleteAt > 0 {
		return nil, newError(ErrChannelNotFound, map[string]any{"Name": channel.Name})
	}
	return channel, nil
}

// checkCaller enforces channel membership, post permission and the optional role restriction.
func (s *Service) checkCaller(callerID string, channel *model.Channel, cfg Config) error {
	member, appErr := s.d.API.GetChannelMember(channel.Id, callerID)
	if appErr != nil || member == nil {
		return newError(ErrNotChannelMember, nil)
	}
	if !s.d.API.HasPermissionToChannel(callerID, channel.Id, model.PermissionCreatePost) {
		return newError(ErrNoPostPermission, nil)
	}

	isSystemAdmin := func() bool {
		return s.d.API.HasPermissionTo(callerID, model.PermissionManageSystem)
	}
	isTeamAdmin := func() bool {
		return channel.TeamId != "" && s.d.API.HasPermissionToTeam(callerID, channel.TeamId, model.PermissionManageTeam)
	}

	var allowed bool
	switch cfg.AllowedCallers {
	case CallersSystemAdmins:
		allowed = isSystemAdmin()
	case CallersTeamAdmins:
		allowed = isTeamAdmin() || isSystemAdmin()
	case CallersChannelAdmins:
		allowed = member.SchemeAdmin || isTeamAdmin() || isSystemAdmin()
	default:
		allowed = true
	}
	if !allowed {
		return newError(ErrRoleNotAllowed, nil)
	}
	return nil
}

// resolveRecipients returns the eligible users (channel members, not the caller, not bots, active)
// and the requested users that were skipped. For whole-channel rings, it stops reading members as
// soon as the maximum is exceeded.
func (s *Service) resolveRecipients(req Request, channel *model.Channel, cfg Config) ([]*model.User, []Skipped, error) {
	var (
		eligible []*model.User
		skipped  []Skipped
		seen     = map[string]bool{req.CallerID: true}
	)

	if req.WholeChannel || req.TargetChannelName != "" {
		for page := 0; ; page++ {
			users, appErr := s.d.API.GetUsersInChannel(channel.Id, model.ChannelSortByUsername, page, channelMembersPerPage)
			if appErr != nil {
				return nil, nil, internalError(appErr)
			}
			for _, u := range users {
				// Bots and deactivated users in a channel ring are dropped silently.
				if seen[u.Id] || u.IsBot || u.DeleteAt > 0 {
					continue
				}
				seen[u.Id] = true
				eligible = append(eligible, u)
			}
			if len(users) < channelMembersPerPage || len(eligible) > cfg.MaxRecipients {
				break
			}
		}
		return eligible, skipped, nil
	}

	var requested []*model.User
	for _, raw := range req.Usernames {
		name := strings.ToLower(strings.TrimPrefix(raw, "@"))
		if name == "" {
			continue
		}
		u, appErr := s.d.API.GetUserByUsername(name)
		if appErr != nil || u == nil {
			skipped = appendSkipped(skipped, Skipped{Username: name, Reason: SkipNotFound})
			continue
		}
		requested = append(requested, u)
	}
	for _, id := range req.UserIDs {
		if !model.IsValidId(id) {
			skipped = appendSkipped(skipped, Skipped{UserID: id, Username: id, Reason: SkipNotFound})
			continue
		}
		u, appErr := s.d.API.GetUser(id)
		if appErr != nil || u == nil {
			skipped = appendSkipped(skipped, Skipped{UserID: id, Username: id, Reason: SkipNotFound})
			continue
		}
		requested = append(requested, u)
	}

	for _, u := range requested {
		if seen[u.Id] {
			continue
		}
		seen[u.Id] = true
		switch {
		case u.IsBot:
			skipped = append(skipped, Skipped{UserID: u.Id, Username: u.Username, Reason: SkipBot})
		case u.DeleteAt > 0:
			skipped = append(skipped, Skipped{UserID: u.Id, Username: u.Username, Reason: SkipInactive})
		default:
			if member, appErr := s.d.API.GetChannelMember(channel.Id, u.Id); appErr != nil || member == nil {
				skipped = append(skipped, Skipped{UserID: u.Id, Username: u.Username, Reason: SkipNotMember})
				continue
			}
			eligible = append(eligible, u)
		}
	}
	return eligible, skipped, nil
}

func appendSkipped(list []Skipped, item Skipped) []Skipped {
	for _, existing := range list {
		if existing.Username == item.Username && existing.Reason == item.Reason {
			return list
		}
	}
	return append(list, item)
}

// classify picks a delivery mode per user from their status. Users without a known status are
// treated as online; the fallback direct message covers them if no webapp acknowledges the ring.
func (s *Service) classify(users []*model.User, cfg Config) (map[string]DeliveryMode, []Skipped) {
	ids := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.Id)
	}
	statusByUser := make(map[string]string, len(users))
	statuses, appErr := s.d.API.GetUserStatusesByIds(ids)
	if appErr != nil {
		s.d.API.LogWarn("Failed to read user statuses, ringing everyone", "err", appErr.Error())
	}
	for _, st := range statuses {
		statusByUser[st.UserId] = st.Status
	}

	modes := make(map[string]DeliveryMode, len(users))
	var skipped []Skipped
	for _, u := range users {
		switch statusByUser[u.Id] {
		case model.StatusOffline:
			modes[u.Id] = DeliveryDM
		case model.StatusDnd:
			switch cfg.DNDBehavior {
			case DNDSkip:
				skipped = append(skipped, Skipped{UserID: u.Id, Username: u.Username, Reason: SkipDND})
			case DNDDMOnly:
				modes[u.Id] = DeliveryDM
			default:
				modes[u.Id] = DeliverySilent
			}
		default:
			modes[u.Id] = DeliveryRing
		}
	}
	return modes, skipped
}

func (s *Service) incomingPayload(r *Ring, rec *Recipient, caller *model.User, channel *model.Channel, cfg Config) map[string]any {
	return map[string]any{
		"ring_id":              r.ID,
		"caller_id":            caller.Id,
		"caller_username":      caller.Username,
		"caller_display_name":  s.displayName(caller),
		"channel_id":           channel.Id,
		"channel_name":         channel.Name,
		"channel_display_name": channel.DisplayName,
		"channel_type":         string(channel.Type),
		"team_id":              channel.TeamId,
		"call_id":              r.CallID,
		"call_post_id":         r.CallPostID,
		"silent":               rec.Mode == DeliverySilent,
		"expires_at":           r.ExpiresAt,
		"timeout_seconds":      int(cfg.RingTimeout.Seconds()),
	}
}

// Respond records a recipient's response and performs its side effects.
func (s *Service) Respond(ringID, userID string, action Action) error {
	var previous RecipientStatus
	r, err := s.d.Store.ModifyRing(ringID, func(r *Ring) error {
		rec, ok := r.Recipients[userID]
		if !ok {
			return newError(ErrNotRecipient, nil)
		}
		previous = rec.Status
		switch action {
		case ActionAck:
			if rec.Status != StatusPending {
				return errNoChange
			}
			rec.Status = StatusDelivered
		case ActionAccept, ActionDecline, ActionDismiss, ActionMissed:
			if rec.Status.IsFinal() {
				return errNoChange
			}
			rec.Status = statusForAction(action)
		default:
			return newError(ErrInvalidRequest, nil)
		}
		return nil
	})
	if errors.Is(err, errNoChange) {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return newError(ErrRingNotFound, nil)
	}
	if err != nil {
		return AsError(err)
	}

	s.d.API.LogDebug("Ring response", "ring_id", r.ID, "user_id", userID, "action", string(action), "previous", string(previous))

	if action == ActionAck {
		return nil
	}

	// Stop ringing on the recipient's other sessions.
	s.d.API.PublishWebSocketEvent(EventRingStop, map[string]any{
		"ring_id": r.ID,
		"reason":  string(action),
	}, &model.WebsocketBroadcast{UserId: userID})

	if action == ActionDecline {
		s.notifyCallerOfDecline(r, userID)
	}
	return nil
}

func statusForAction(a Action) RecipientStatus {
	switch a {
	case ActionAccept:
		return StatusAccepted
	case ActionDecline:
		return StatusDeclined
	case ActionDismiss:
		return StatusDismissed
	case ActionMissed:
		return StatusMissed
	}
	return StatusPending
}

func (s *Service) notifyCallerOfDecline(r *Ring, userID string) {
	user, appErr := s.d.API.GetUser(userID)
	if appErr != nil {
		return
	}
	caller, appErr := s.d.API.GetUser(r.CallerID)
	if appErr != nil {
		return
	}
	s.d.API.SendEphemeralPost(r.CallerID, &model.Post{
		UserId:    s.botUserID(),
		ChannelId: r.ChannelID,
		Message:   s.d.Translate(caller.Locale, "ring.declined", map[string]any{"Name": "@" + user.Username}),
	})
}

// Cancel stops a ring started by callerID. When ringID is empty, the caller's latest ring is
// cancelled. It returns the number of recipients that were still being rung.
func (s *Service) Cancel(callerID, ringID string) (int, error) {
	if ringID == "" {
		latest, err := s.d.Store.GetLatestRing(callerID)
		if errors.Is(err, ErrNotFound) {
			return 0, newError(ErrRingNotFound, nil)
		}
		if err != nil {
			return 0, internalError(err)
		}
		ringID = latest
	}

	var stopped []string
	r, err := s.d.Store.ModifyRing(ringID, func(r *Ring) error {
		if r.CallerID != callerID {
			return newError(ErrNotCaller, nil)
		}
		if r.Cancelled {
			return errNoChange
		}
		r.Cancelled = true
		stopped = stopped[:0]
		for userID, rec := range r.Recipients {
			if !rec.Status.IsFinal() {
				rec.Status = StatusCancelled
				stopped = append(stopped, userID)
			}
		}
		return nil
	})
	if errors.Is(err, errNoChange) {
		return 0, nil
	}
	if errors.Is(err, ErrNotFound) {
		return 0, newError(ErrRingNotFound, nil)
	}
	if err != nil {
		return 0, AsError(err)
	}

	for _, userID := range stopped {
		s.d.API.PublishWebSocketEvent(EventRingStop, map[string]any{
			"ring_id": r.ID,
			"reason":  "cancelled",
		}, &model.WebsocketBroadcast{UserId: userID})
	}
	s.d.API.LogInfo("Ring cancelled", "ring_id", r.ID, "caller_id", callerID, "channel_id", r.ChannelID, "stopped_count", len(stopped))
	return len(stopped), nil
}

// SetCallID attaches the Calls call ID, reported by the caller's webapp once the call started,
// and forwards it to recipients that are still being rung.
func (s *Service) SetCallID(ringID, callerID, callID string) error {
	if !model.IsValidId(callID) {
		return newError(ErrInvalidRequest, nil)
	}
	r, err := s.d.Store.ModifyRing(ringID, func(r *Ring) error {
		if r.CallerID != callerID {
			return newError(ErrNotCaller, nil)
		}
		if r.CallID == callID {
			return errNoChange
		}
		r.CallID = callID
		return nil
	})
	if errors.Is(err, errNoChange) {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return newError(ErrRingNotFound, nil)
	}
	if err != nil {
		return AsError(err)
	}

	if err := s.d.Calls.SetCallID(r.ChannelID, callID); err != nil {
		s.d.API.LogWarn("Failed to record call id", "channel_id", r.ChannelID, "err", err.Error())
	}

	payload := map[string]any{"ring_id": r.ID, "call_id": callID}
	if call, callErr := s.d.Calls.ActiveCall(r.ChannelID); callErr == nil && call != nil {
		payload["call_post_id"] = call.PostID
	}
	for userID, rec := range r.Recipients {
		if rec.Mode != DeliveryDM && !rec.Status.IsFinal() {
			s.d.API.PublishWebSocketEvent(EventRingUpdated, payload, &model.WebsocketBroadcast{UserId: userID})
		}
	}
	return nil
}

// SendFallbackMessages sends a direct message to in-app recipients whose webapp never acknowledged
// the ring, for example because they only use the mobile app. It runs once per ring, after the
// fallback delay.
func (s *Service) SendFallbackMessages(ringID string) {
	var targets []string
	_, err := s.d.Store.ModifyRing(ringID, func(r *Ring) error {
		targets = targets[:0]
		if r.Cancelled {
			return errNoChange
		}
		for userID, rec := range r.Recipients {
			if rec.Mode != DeliveryDM && rec.Status == StatusPending && !rec.DMSent {
				rec.DMSent = true
				targets = append(targets, userID)
			}
		}
		if len(targets) == 0 {
			return errNoChange
		}
		return nil
	})
	if errors.Is(err, errNoChange) || errors.Is(err, ErrNotFound) {
		return
	}
	if err != nil {
		s.d.API.LogWarn("Failed to prepare fallback messages", "ring_id", ringID, "err", err.Error())
		return
	}
	s.deliverDirectMessages(ringID, targets)
}

// sendDirectMessages sends the direct message to DM-only recipients right after the ring started.
func (s *Service) sendDirectMessages(ringID string, userIDs []string) {
	want := make(map[string]bool, len(userIDs))
	for _, id := range userIDs {
		want[id] = true
	}
	var targets []string
	_, err := s.d.Store.ModifyRing(ringID, func(r *Ring) error {
		targets = targets[:0]
		if r.Cancelled {
			return errNoChange
		}
		for userID, rec := range r.Recipients {
			if want[userID] && !rec.DMSent {
				rec.DMSent = true
				targets = append(targets, userID)
			}
		}
		if len(targets) == 0 {
			return errNoChange
		}
		return nil
	})
	if errors.Is(err, errNoChange) || errors.Is(err, ErrNotFound) {
		return
	}
	if err != nil {
		s.d.API.LogWarn("Failed to prepare direct messages", "ring_id", ringID, "err", err.Error())
		return
	}
	s.deliverDirectMessages(ringID, targets)
}

func (s *Service) deliverDirectMessages(ringID string, userIDs []string) {
	r, err := s.d.Store.GetRing(ringID)
	if err != nil {
		s.d.API.LogWarn("Failed to read ring for direct messages", "ring_id", ringID, "err", err.Error())
		return
	}
	caller, appErr := s.d.API.GetUser(r.CallerID)
	if appErr != nil {
		s.d.API.LogWarn("Failed to read caller for direct messages", "ring_id", ringID, "err", appErr.Error())
		return
	}
	channel, appErr := s.d.API.GetChannel(r.ChannelID)
	if appErr != nil {
		s.d.API.LogWarn("Failed to read channel for direct messages", "ring_id", ringID, "err", appErr.Error())
		return
	}
	// The call usually started after the ring was sent, so look up the call post again.
	if call, callErr := s.d.Calls.ActiveCall(r.ChannelID); callErr == nil && call != nil && call.PostID != "" {
		r.CallPostID = call.PostID
	}

	botID := s.botUserID()
	sent := 0
	for _, userID := range userIDs {
		if s.sendDirectMessage(r, botID, caller, channel, userID) {
			sent++
		}
	}
	s.d.API.LogDebug("Sent ring direct messages", "ring_id", ringID, "count", sent)
}

func (s *Service) sendDirectMessage(r *Ring, botID string, caller *model.User, channel *model.Channel, userID string) bool {
	recipient, appErr := s.d.API.GetUser(userID)
	if appErr != nil {
		return false
	}
	dm, appErr := s.d.API.GetDirectChannel(userID, botID)
	if appErr != nil {
		s.d.API.LogWarn("Failed to open direct channel", "user_id", userID, "err", appErr.Error())
		return false
	}

	params := map[string]any{
		"Caller": s.displayName(caller),
		"Link":   s.joinLink(r, channel, caller, recipient),
	}
	messageID := "ring.dm.channel"
	switch channel.Type {
	case model.ChannelTypeDirect:
		messageID = "ring.dm.direct"
	case model.ChannelTypeGroup:
		messageID = "ring.dm.group"
	default:
		params["Channel"] = channel.DisplayName
	}

	_, appErr = s.d.API.CreatePost(&model.Post{
		UserId:    botID,
		ChannelId: dm.Id,
		Message:   s.d.Translate(recipient.Locale, messageID, params),
		Props: model.StringInterface{
			"group_ring_id": r.ID,
		},
	})
	if appErr != nil {
		s.d.API.LogWarn("Failed to send ring direct message", "user_id", userID, "err", appErr.Error())
		return false
	}
	return true
}

// joinLink builds a link that opens the call: the call post when known, otherwise the channel.
func (s *Service) joinLink(r *Ring, channel *model.Channel, caller, recipient *model.User) string {
	siteURL := strings.TrimRight(s.siteURL(), "/")

	teamName := ""
	if channel.TeamId != "" {
		if team, appErr := s.d.API.GetTeam(channel.TeamId); appErr == nil {
			teamName = team.Name
		}
	} else if teams, appErr := s.d.API.GetTeamsForUser(recipient.Id); appErr == nil && len(teams) > 0 {
		teamName = teams[0].Name
	}

	if r.CallPostID != "" {
		if teamName != "" {
			return siteURL + "/" + teamName + "/pl/" + r.CallPostID
		}
		return siteURL + "/_redirect/pl/" + r.CallPostID
	}
	if teamName == "" {
		return siteURL
	}
	switch channel.Type {
	case model.ChannelTypeDirect:
		return siteURL + "/" + teamName + "/messages/@" + caller.Username
	case model.ChannelTypeGroup:
		return siteURL + "/" + teamName + "/messages/" + channel.Name
	}
	return siteURL + "/" + teamName + "/channels/" + channel.Name
}

func (s *Service) displayName(u *model.User) string {
	format := model.ShowUsername
	if cfg := s.d.API.GetConfig(); cfg != nil {
		if cfg.TeamSettings.TeammateNameDisplay != nil {
			format = *cfg.TeamSettings.TeammateNameDisplay
		}
		if cfg.PrivacySettings.ShowFullName != nil && !*cfg.PrivacySettings.ShowFullName && format == model.ShowFullName {
			format = model.ShowUsername
		}
	}
	return u.GetDisplayName(format)
}

func (s *Service) botUserID() string {
	if s.d.BotUserID == nil {
		return ""
	}
	return s.d.BotUserID()
}

func (s *Service) siteURL() string {
	if s.d.SiteURL == nil {
		return ""
	}
	return s.d.SiteURL()
}

func isDirectOrGroup(c *model.Channel) bool {
	return c.Type == model.ChannelTypeDirect || c.Type == model.ChannelTypeGroup
}
