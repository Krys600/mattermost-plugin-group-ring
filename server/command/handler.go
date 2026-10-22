package command

import (
	"errors"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/Krys600/mattermost-plugin-group-ring/server/ring"
)

// Outcome kinds, also used in the REST API response.
const (
	KindHelp              = "help"
	KindRinging           = "ringing"
	KindDeferred          = "deferred_to_native"
	KindNeedsConfirmation = "needs_confirmation"
	KindCancelled         = "cancelled"
	KindError             = "error"
)

// SourceServerCommand marks commands executed by the server-side slash command, which clients
// without the webapp plugin (such as the mobile apps) use.
const SourceServerCommand = "command"

// maxListedNames caps how many usernames a reply lists before summarizing the rest.
const maxListedNames = 10

// Service is the part of the ring service the command needs.
type Service interface {
	Start(req ring.Request) (*ring.Result, error)
	Cancel(callerID, ringID string) (int, error)
}

// Input is a command invocation, from the server-side slash command or the webapp.
type Input struct {
	UserID    string
	ChannelID string
	TeamID    string
	Text      string
	Locale    string
	// Confirmed is set by the webapp after the caller confirmed a large ring.
	Confirmed bool
	// CallActiveHint is set by the webapp when it knows a call is running in the channel.
	CallActiveHint bool
	// GroupCallsAllowedHint is the Calls "GroupCallsAllowed" setting as seen by the webapp.
	GroupCallsAllowedHint *bool
	Source                string
}

// Outcome is the result of running a command. Text is the localized reply for the caller.
type Outcome struct {
	Kind      string       `json:"kind"`
	Text      string       `json:"text"`
	Result    *ring.Result `json:"result,omitempty"`
	ErrorCode string       `json:"error_code,omitempty"`
	// Status is the HTTP status that matches the outcome.
	Status int `json:"-"`
}

// Handler runs /ring commands.
type Handler struct {
	svc Service
	t   ring.Translator
}

// NewHandler creates a command handler.
func NewHandler(svc Service, t ring.Translator) *Handler {
	return &Handler{svc: svc, t: t}
}

// AutocompleteData describes the command for the client-side autocomplete.
func AutocompleteData() *model.AutocompleteData {
	data := model.NewAutocompleteData(Trigger, "[@user ...] | @channel | ~channel | --cancel | --help", "Ring people to join a call in this channel")
	data.AddTextArgument("Users to ring, @channel for all channel members, or ~channel-name for another channel", "[@user ...] | @channel | ~channel", "")
	return data
}

// Run parses and executes a command.
func (h *Handler) Run(in Input) *Outcome {
	parsed, err := Parse(in.Text)
	if err != nil {
		var perr *ParseError
		if errors.As(err, &perr) {
			text := h.t(in.Locale, perr.MessageID, perr.Params) + "\n\n" + h.t(in.Locale, "command.usage", nil)
			return &Outcome{Kind: KindError, Text: text, ErrorCode: string(ring.ErrInvalidRequest), Status: 400}
		}
		return h.ErrorOutcome(in.Locale, err)
	}

	if parsed.Help {
		return &Outcome{Kind: KindHelp, Text: h.t(in.Locale, "command.help", nil), Status: 200}
	}

	if parsed.Cancel {
		count, cancelErr := h.svc.Cancel(in.UserID, "")
		if cancelErr != nil {
			return h.ErrorOutcome(in.Locale, cancelErr)
		}
		return &Outcome{Kind: KindCancelled, Text: h.t(in.Locale, "command.cancelled", map[string]any{"Count": count}), Status: 200}
	}

	req := ring.Request{
		CallerID:              in.UserID,
		ChannelID:             in.ChannelID,
		TeamID:                in.TeamID,
		Usernames:             parsed.Usernames,
		WholeChannel:          parsed.WholeChannel,
		TargetChannelName:     parsed.ChannelName,
		Confirmed:             parsed.Confirm || in.Confirmed,
		CallActiveHint:        in.CallActiveHint,
		GroupCallsAllowedHint: in.GroupCallsAllowedHint,
		Source:                in.Source,
	}
	result, err := h.svc.Start(req)
	if err != nil {
		return h.ErrorOutcome(in.Locale, err)
	}
	outcome := h.ResultOutcome(in.Locale, result, in.Text)
	// Without the webapp plugin (for example on mobile) nothing starts the call for the caller.
	if in.Source == SourceServerCommand && (outcome.Kind == KindRinging || outcome.Kind == KindDeferred) {
		outcome.Text += "\n" + h.t(in.Locale, "command.start_call_hint", nil)
	}
	return outcome
}

// ResultOutcome turns a successful ring result into a reply. commandText is used to suggest the
// confirmation command when the request needs confirmation.
func (h *Handler) ResultOutcome(locale string, result *ring.Result, commandText string) *Outcome {
	if result.NeedsConfirmation {
		cmd := strings.TrimSpace(commandText)
		if cmd == "" {
			cmd = "/" + Trigger + " @channel"
		} else if !strings.HasPrefix(cmd, "/") {
			cmd = "/" + Trigger + " " + cmd
		}
		return &Outcome{
			Kind: KindNeedsConfirmation,
			Text: h.t(locale, "command.needs_confirmation", map[string]any{
				"Count":   result.RecipientCount,
				"Command": cmd + " --confirm",
			}),
			Result: result,
			Status: 200,
		}
	}

	var lines []string
	if result.DeferredToNative {
		lines = append(lines, h.t(locale, "command.deferred", nil))
	} else {
		channelName := ""
		if result.Channel != nil {
			channelName = result.Channel.Name
		}
		if len(result.Rung) > 0 {
			lines = append(lines, h.t(locale, "command.ringing", map[string]any{
				"Count": len(result.Rung), "Names": h.names(locale, result.Rung), "Channel": channelName,
			}))
		}
		if len(result.Silent) > 0 {
			lines = append(lines, h.t(locale, "command.ringing_silent", map[string]any{"Names": h.names(locale, result.Silent)}))
		}
		if len(result.DMOnly) > 0 {
			lines = append(lines, h.t(locale, "command.dm_only", map[string]any{"Names": h.names(locale, result.DMOnly)}))
		}
	}
	if skippedText := h.skipped(locale, result.Skipped); skippedText != "" {
		lines = append(lines, skippedText)
	}
	if !result.DeferredToNative {
		lines = append(lines, h.t(locale, "command.cancel_hint", nil))
	}

	kind := KindRinging
	if result.DeferredToNative {
		kind = KindDeferred
	}
	return &Outcome{Kind: kind, Text: strings.Join(lines, "\n"), Result: result, Status: 200}
}

// ErrorOutcome turns an error into a reply.
func (h *Handler) ErrorOutcome(locale string, err error) *Outcome {
	e := ring.AsError(err)
	text := h.t(locale, "ring.error."+string(e.Code), e.Params)
	if e.Code == ring.ErrNoRecipients {
		if skippedText := h.skipped(locale, e.Skipped); skippedText != "" {
			text += "\n" + skippedText
		}
	}
	if e.Code == ring.ErrInvalidRequest || e.Code == ring.ErrMixedTargets {
		text += "\n\n" + h.t(locale, "command.usage", nil)
	}
	return &Outcome{Kind: KindError, Text: text, ErrorCode: string(e.Code), Status: e.HTTPStatus()}
}

func (h *Handler) names(locale string, usernames []string) string {
	listed := usernames
	if len(listed) > maxListedNames {
		listed = listed[:maxListedNames]
	}
	parts := make([]string, 0, len(listed))
	for _, name := range listed {
		parts = append(parts, "@"+name)
	}
	text := strings.Join(parts, ", ")
	if extra := len(usernames) - len(listed); extra > 0 {
		text += " " + h.t(locale, "command.and_more", map[string]any{"Count": extra})
	}
	return text
}

func (h *Handler) skipped(locale string, skipped []ring.Skipped) string {
	if len(skipped) == 0 {
		return ""
	}
	parts := make([]string, 0, len(skipped))
	for i, s := range skipped {
		if i == maxListedNames {
			parts = append(parts, h.t(locale, "command.and_more", map[string]any{"Count": len(skipped) - maxListedNames}))
			break
		}
		parts = append(parts, h.t(locale, "command.skipped_entry", map[string]any{
			"Name":   "@" + s.Username,
			"Reason": h.t(locale, "ring.skip."+string(s.Reason), nil),
		}))
	}
	return h.t(locale, "command.skipped", map[string]any{"Entries": strings.Join(parts, ", ")})
}
