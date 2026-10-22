package command

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		text string
		want *Parsed
		err  string
	}{
		{"users", "/ring @alice @bob", &Parsed{Usernames: []string{"alice", "bob"}}, ""},
		{"users with commas and duplicates", "/ring @Alice, @bob,@alice", &Parsed{Usernames: []string{"alice", "bob"}}, ""},
		{"without trigger", "@alice", &Parsed{Usernames: []string{"alice"}}, ""},
		{"current channel", "/ring @channel", &Parsed{WholeChannel: true}, ""},
		{"current channel alias", "/ring @all", &Parsed{WholeChannel: true}, ""},
		{"named channel", "/ring ~Dev-Team", &Parsed{ChannelName: "dev-team"}, ""},
		{"confirm", "/ring @channel --confirm", &Parsed{WholeChannel: true, Confirm: true}, ""},
		{"help", "/ring --help", &Parsed{Help: true}, ""},
		{"help wins over errors in targets", "/ring @alice ~dev --help", &Parsed{Help: true, Usernames: []string{"alice"}, ChannelName: "dev"}, ""},
		{"cancel", "/ring --cancel", &Parsed{Cancel: true}, ""},
		{"empty", "/ring", nil, "command.error.no_targets"},
		{"unknown option", "/ring --loud @alice", nil, "command.error.unknown_option"},
		{"bare word", "/ring alice", nil, "command.error.unexpected_argument"},
		{"mixed", "/ring @alice @channel", nil, "command.error.mixed_targets"},
		{"mixed with named channel", "/ring ~dev @alice", nil, "command.error.mixed_targets"},
		{"two channels", "/ring ~dev ~ops", nil, "command.error.multiple_channels"},
		{"current and named channel", "/ring @channel ~dev", nil, "command.error.multiple_channels"},
		{"empty channel", "/ring ~", nil, "command.error.empty_channel"},
		{"cancel with targets", "/ring --cancel @alice", nil, "command.error.cancel_with_targets"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.text)
			if tc.err != "" {
				var perr *ParseError
				require.True(t, errors.As(err, &perr), "expected a ParseError, got %v", err)
				assert.Equal(t, tc.err, perr.MessageID)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
