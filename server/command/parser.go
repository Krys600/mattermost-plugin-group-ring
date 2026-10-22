// Package command implements the /ring slash command: parsing, execution against the ring
// service and the user-facing replies.
package command

import (
	"strings"
	"unicode"
)

// Trigger is the slash command trigger.
const Trigger = "ring"

// Parsed is the structured form of a /ring command line.
type Parsed struct {
	Help         bool
	Cancel       bool
	Confirm      bool
	WholeChannel bool
	// ChannelName is set for "~channel-name" (without the tilde).
	ChannelName string
	// Usernames are the mentioned users (without the @).
	Usernames []string
}

// ParseError is a user error in the command line. MessageID refers to the i18n catalog.
type ParseError struct {
	MessageID string
	Params    map[string]any
}

func (e *ParseError) Error() string {
	return e.MessageID
}

// Parse parses the text of a /ring command, with or without the leading "/ring".
func Parse(text string) (*Parsed, error) {
	// Accept separators such as "@alice, @bob" or "@alice,@bob".
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';'
	})
	if len(fields) > 0 && strings.EqualFold(fields[0], "/"+Trigger) {
		fields = fields[1:]
	}

	p := &Parsed{}
	seen := map[string]bool{}
	for _, token := range fields {
		lower := strings.ToLower(token)

		switch {
		case lower == "--help" || lower == "-h" || lower == "help":
			p.Help = true
		case lower == "--cancel" || lower == "cancel":
			p.Cancel = true
		case lower == "--confirm" || lower == "--yes" || lower == "-y":
			p.Confirm = true
		case strings.HasPrefix(lower, "-"):
			return nil, &ParseError{MessageID: "command.error.unknown_option", Params: map[string]any{"Option": token}}
		case lower == "@channel" || lower == "@all":
			p.WholeChannel = true
		case strings.HasPrefix(lower, "~"):
			name := strings.TrimPrefix(lower, "~")
			if name == "" {
				return nil, &ParseError{MessageID: "command.error.empty_channel"}
			}
			if p.ChannelName != "" && p.ChannelName != name {
				return nil, &ParseError{MessageID: "command.error.multiple_channels"}
			}
			p.ChannelName = name
		case strings.HasPrefix(lower, "@"):
			name := strings.TrimPrefix(lower, "@")
			if name == "" {
				continue
			}
			if !seen[name] {
				seen[name] = true
				p.Usernames = append(p.Usernames, name)
			}
		default:
			return nil, &ParseError{MessageID: "command.error.unexpected_argument", Params: map[string]any{"Argument": token}}
		}
	}

	if p.Help {
		return p, nil
	}
	if p.Cancel {
		if p.WholeChannel || p.ChannelName != "" || len(p.Usernames) > 0 {
			return nil, &ParseError{MessageID: "command.error.cancel_with_targets"}
		}
		return p, nil
	}
	if (p.WholeChannel || p.ChannelName != "") && len(p.Usernames) > 0 {
		return nil, &ParseError{MessageID: "command.error.mixed_targets"}
	}
	if p.WholeChannel && p.ChannelName != "" {
		return nil, &ParseError{MessageID: "command.error.multiple_channels"}
	}
	if !p.WholeChannel && p.ChannelName == "" && len(p.Usernames) == 0 {
		return nil, &ParseError{MessageID: "command.error.no_targets"}
	}
	return p, nil
}
