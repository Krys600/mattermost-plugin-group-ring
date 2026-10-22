package main

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"

	"github.com/Krys600/mattermost-plugin-group-ring/server/command"
	"github.com/Krys600/mattermost-plugin-group-ring/server/ring"
)

// clientConfig is the configuration the webapp needs to render its UI.
type clientConfig struct {
	Enabled            bool            `json:"enabled"`
	RingTimeoutSeconds int             `json:"ring_timeout_seconds"`
	MaxRecipients      int             `json:"max_recipients"`
	ConfirmThreshold   int             `json:"confirm_threshold"`
	ChannelTypes       map[string]bool `json:"channel_types"`
	AllowedCallers     string          `json:"allowed_callers"`
	CallsAvailable     bool            `json:"calls_available"`
}

type startRingRequest struct {
	ChannelID string `json:"channel_id"`
	TeamID    string `json:"team_id"`
	// Command is the raw text of a /ring command intercepted by the webapp.
	Command      string   `json:"command"`
	UserIDs      []string `json:"user_ids"`
	WholeChannel bool     `json:"whole_channel"`
	Confirmed    bool     `json:"confirmed"`
	CallActive   bool     `json:"call_active"`
	// GroupCallsAllowed is the Calls plugin's client setting, when the webapp could read it.
	GroupCallsAllowed *bool `json:"group_calls_allowed"`
}

type respondRequest struct {
	Action string `json:"action"`
}

type setCallIDRequest struct {
	CallID string `json:"call_id"`
}

type errorResponse struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

func userID(r *http.Request) string {
	return r.Header.Get("Mattermost-User-ID")
}

func (p *Plugin) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		requestLogger(r).WithError(err).Warnf("Failed to write response")
	}
}

func (p *Plugin) writeError(w http.ResponseWriter, r *http.Request, err error) {
	e := ring.AsError(err)
	if e.Code == ring.ErrInternal {
		requestLogger(r).WithError(err).Errorf("Request failed")
	}
	p.writeJSON(w, r, e.HTTPStatus(), errorResponse{
		ErrorCode: string(e.Code),
		Message:   p.i18n.T(p.userLocale(userID(r)), "ring.error."+string(e.Code), e.Params),
	})
}

func (p *Plugin) sendEphemeral(userID, channelID, message string) {
	if message == "" {
		return
	}
	p.API.SendEphemeralPost(userID, &model.Post{
		UserId:    p.botUserID,
		ChannelId: channelID,
		Message:   message,
	})
}

func (p *Plugin) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := p.getConfiguration().ringConfig()
	p.writeJSON(w, r, http.StatusOK, clientConfig{
		Enabled:            cfg.Enabled,
		RingTimeoutSeconds: int(cfg.RingTimeout.Seconds()),
		MaxRecipients:      cfg.MaxRecipients,
		ConfirmThreshold:   cfg.ConfirmThreshold,
		ChannelTypes: map[string]bool{
			string(model.ChannelTypeOpen):    cfg.ChannelTypes.Public,
			string(model.ChannelTypePrivate): cfg.ChannelTypes.Private,
			string(model.ChannelTypeDirect):  cfg.ChannelTypes.Direct,
			string(model.ChannelTypeGroup):   cfg.ChannelTypes.Group,
		},
		AllowedCallers: cfg.AllowedCallers,
		CallsAvailable: p.calls.Available() == nil,
	})
}

func (p *Plugin) handleStartRing(w http.ResponseWriter, r *http.Request) {
	callerID := userID(r)

	var req startRingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.writeError(w, r, &ring.Error{Code: ring.ErrInvalidRequest})
		return
	}
	if !model.IsValidId(req.ChannelID) || (req.TeamID != "" && !model.IsValidId(req.TeamID)) || len(req.UserIDs) > maxMaxRecipients {
		p.writeError(w, r, &ring.Error{Code: ring.ErrInvalidRequest})
		return
	}

	locale := p.userLocale(callerID)

	if req.Command != "" {
		outcome := p.commands.Run(command.Input{
			UserID:                callerID,
			ChannelID:             req.ChannelID,
			TeamID:                req.TeamID,
			Text:                  req.Command,
			Locale:                locale,
			Confirmed:             req.Confirmed,
			CallActiveHint:        req.CallActive,
			GroupCallsAllowedHint: req.GroupCallsAllowed,
			Source:                "webapp_command",
		})
		// The webapp asks for confirmation itself; every other outcome is replied to in the channel,
		// like a server-side slash command.
		if outcome.Kind != command.KindNeedsConfirmation {
			p.sendEphemeral(callerID, req.ChannelID, outcome.Text)
		}
		p.writeJSON(w, r, outcome.Status, outcome)
		return
	}

	result, err := p.ring.Start(ring.Request{
		CallerID:              callerID,
		ChannelID:             req.ChannelID,
		TeamID:                req.TeamID,
		UserIDs:               req.UserIDs,
		WholeChannel:          req.WholeChannel,
		Confirmed:             req.Confirmed,
		CallActiveHint:        req.CallActive,
		GroupCallsAllowedHint: req.GroupCallsAllowed,
		Source:                "webapp",
	})
	if err != nil {
		// The webapp modal shows the error inline.
		outcome := p.commands.ErrorOutcome(locale, err)
		p.writeJSON(w, r, outcome.Status, outcome)
		return
	}
	outcome := p.commands.ResultOutcome(locale, result, "")
	if outcome.Kind != command.KindNeedsConfirmation {
		p.sendEphemeral(callerID, result.ChannelID, outcome.Text)
	}
	p.writeJSON(w, r, outcome.Status, outcome)
}

func (p *Plugin) handleRespond(w http.ResponseWriter, r *http.Request) {
	var req respondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.writeError(w, r, &ring.Error{Code: ring.ErrInvalidRequest})
		return
	}
	action, ok := ring.ParseAction(req.Action)
	if !ok {
		p.writeError(w, r, &ring.Error{Code: ring.ErrInvalidRequest})
		return
	}
	if err := p.ring.Respond(mux.Vars(r)["ring_id"], userID(r), action); err != nil {
		p.writeError(w, r, err)
		return
	}
	p.writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Plugin) handleCancel(w http.ResponseWriter, r *http.Request) {
	stopped, err := p.ring.Cancel(userID(r), mux.Vars(r)["ring_id"])
	if err != nil {
		p.writeError(w, r, err)
		return
	}
	p.writeJSON(w, r, http.StatusOK, map[string]int{"stopped": stopped})
}

func (p *Plugin) handleSetCallID(w http.ResponseWriter, r *http.Request) {
	var req setCallIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.writeError(w, r, &ring.Error{Code: ring.ErrInvalidRequest})
		return
	}
	if err := p.ring.SetCallID(mux.Vars(r)["ring_id"], userID(r), req.CallID); err != nil {
		p.writeError(w, r, err)
		return
	}
	p.writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}
