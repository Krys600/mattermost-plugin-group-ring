package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/utaku/mattermost-plugin-group-ring/server/ring"
)

func TestRingConfigDefaultsAndClamping(t *testing.T) {
	cfg := (&configuration{}).ringConfig()
	assert.Equal(t, 30*time.Second, cfg.RingTimeout)
	assert.Equal(t, 50, cfg.MaxRecipients)
	assert.Equal(t, ring.CallersEveryone, cfg.AllowedCallers)
	assert.Equal(t, ring.DNDSilent, cfg.DNDBehavior)

	cfg = (&configuration{
		RingTimeoutSeconds: 1,
		MaxRecipients:      5000,
		ConfirmThreshold:   -3,
		RateLimitPerMinute: 1000,
		AllowedCallers:     "nobody",
		DNDBehavior:        "loud",
	}).ringConfig()
	assert.Equal(t, 5*time.Second, cfg.RingTimeout)
	assert.Equal(t, 1000, cfg.MaxRecipients)
	assert.Equal(t, 0, cfg.ConfirmThreshold)
	assert.Equal(t, 60, cfg.RateLimitPerMinute)
	assert.Equal(t, ring.CallersEveryone, cfg.AllowedCallers)
	assert.Equal(t, ring.DNDSilent, cfg.DNDBehavior)

	cfg = (&configuration{RingTimeoutSeconds: 900, AllowGroupMessages: true, AllowedCallers: ring.CallersTeamAdmins}).ringConfig()
	assert.Equal(t, 300*time.Second, cfg.RingTimeout)
	assert.Equal(t, ring.ChannelTypes{Group: true}, cfg.ChannelTypes)
	assert.Equal(t, ring.CallersTeamAdmins, cfg.AllowedCallers)
}
