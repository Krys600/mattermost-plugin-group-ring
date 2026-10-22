// Package kvstore persists the plugin's transient state in the plugin KV store.
// Every record is written with an expiry; nothing is kept permanently.
package kvstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/Krys600/mattermost-plugin-group-ring/server/ring"
)

const (
	ringKeyPrefix      = "ring_"
	latestRingPrefix   = "latest_"
	rateLimitKeyPrefix = "rl_"

	// maxCASAttempts bounds the optimistic-concurrency retry loop.
	maxCASAttempts = 10
)

// API is the subset of the plugin API used by the store.
type API interface {
	KVSetWithOptions(key string, value []byte, options model.PluginKVSetOptions) (bool, *model.AppError)
	KVGet(key string) ([]byte, *model.AppError)
}

// Store implements ring.Store on top of the plugin KV store.
type Store struct {
	api API
	now func() time.Time
}

var _ ring.Store = (*Store)(nil)

// New creates a KV-backed store.
func New(api API) *Store {
	return &Store{api: api, now: time.Now}
}

func ringKey(id string) string {
	return ringKeyPrefix + id
}

// ringTTL returns the expiry of a ring record: the ring timeout plus the retention period.
func (s *Store) ringTTL(r *ring.Ring) int64 {
	remaining := time.UnixMilli(r.ExpiresAt).Sub(s.now()) + ring.RecordRetention
	return max(int64(remaining.Seconds()), 1)
}

func (s *Store) SaveRing(r *ring.Ring) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, appErr := s.api.KVSetWithOptions(ringKey(r.ID), data, model.PluginKVSetOptions{ExpireInSeconds: s.ringTTL(r)}); appErr != nil {
		return appErr
	}
	return nil
}

func (s *Store) getRingRaw(id string) (*ring.Ring, []byte, error) {
	if !model.IsValidId(id) {
		return nil, nil, ring.ErrNotFound
	}
	data, appErr := s.api.KVGet(ringKey(id))
	if appErr != nil {
		return nil, nil, appErr
	}
	if data == nil {
		return nil, nil, ring.ErrNotFound
	}
	var r ring.Ring
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, nil, err
	}
	return &r, data, nil
}

func (s *Store) GetRing(id string) (*ring.Ring, error) {
	r, _, err := s.getRingRaw(id)
	return r, err
}

func (s *Store) ModifyRing(id string, fn func(*ring.Ring) error) (*ring.Ring, error) {
	for range maxCASAttempts {
		r, old, err := s.getRingRaw(id)
		if err != nil {
			return nil, err
		}
		if err = fn(r); err != nil {
			return r, err
		}
		data, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(data, old) {
			return r, nil
		}
		ok, appErr := s.api.KVSetWithOptions(ringKey(id), data, model.PluginKVSetOptions{
			Atomic:          true,
			OldValue:        old,
			ExpireInSeconds: s.ringTTL(r),
		})
		if appErr != nil {
			return nil, appErr
		}
		if ok {
			return r, nil
		}
	}
	return nil, fmt.Errorf("failed to update ring %s: too many concurrent updates", id)
}

func (s *Store) SetLatestRing(callerID, ringID string, ttl time.Duration) error {
	if _, appErr := s.api.KVSetWithOptions(latestRingPrefix+callerID, []byte(ringID), model.PluginKVSetOptions{
		ExpireInSeconds: int64(ttl.Seconds()),
	}); appErr != nil {
		return appErr
	}
	return nil
}

func (s *Store) GetLatestRing(callerID string) (string, error) {
	data, appErr := s.api.KVGet(latestRingPrefix + callerID)
	if appErr != nil {
		return "", appErr
	}
	if len(data) == 0 {
		return "", ring.ErrNotFound
	}
	return string(data), nil
}

// TakeRateLimitToken implements a sliding-window rate limit. The record holds the timestamps
// (Unix milliseconds) of the rings within the window and is updated with compare-and-set, so the
// limit holds across cluster nodes.
func (s *Store) TakeRateLimitToken(userID string, limit int, window time.Duration, now time.Time) (bool, time.Duration, error) {
	key := rateLimitKeyPrefix + userID
	for range maxCASAttempts {
		old, appErr := s.api.KVGet(key)
		if appErr != nil {
			return false, 0, appErr
		}

		var stamps []int64
		if old != nil {
			if err := json.Unmarshal(old, &stamps); err != nil {
				// A corrupt record must not lock the user out; start over.
				stamps = nil
			}
		}

		cutoff := now.Add(-window).UnixMilli()
		recent := stamps[:0:0]
		for _, ts := range stamps {
			if ts > cutoff {
				recent = append(recent, ts)
			}
		}

		if len(recent) >= limit {
			oldest := recent[0]
			for _, ts := range recent {
				oldest = min(oldest, ts)
			}
			retryAfter := time.UnixMilli(oldest).Add(window).Sub(now)
			return false, max(retryAfter, time.Second), nil
		}

		recent = append(recent, now.UnixMilli())
		data, err := json.Marshal(recent)
		if err != nil {
			return false, 0, err
		}

		opts := model.PluginKVSetOptions{Atomic: true, OldValue: old, ExpireInSeconds: int64(window.Seconds()) + 1}
		ok, appErr := s.api.KVSetWithOptions(key, data, opts)
		if appErr != nil {
			return false, 0, appErr
		}
		if ok {
			return true, 0, nil
		}
	}
	return false, 0, errors.New("failed to update rate limit: too many concurrent updates")
}
