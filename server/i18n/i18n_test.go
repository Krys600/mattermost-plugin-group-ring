package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundle(t *testing.T) {
	b, err := Load()
	require.NoError(t, err)

	t.Run("substitutes parameters", func(t *testing.T) {
		assert.Equal(t, "@bob declined your call.", b.T("en", "ring.declined", map[string]any{"Name": "@bob"}))
	})

	t.Run("falls back to English for unknown locales", func(t *testing.T) {
		assert.Equal(t, "@bob declined your call.", b.T("pt-BR", "ring.declined", map[string]any{"Name": "@bob"}))
	})

	t.Run("returns the id for unknown messages", func(t *testing.T) {
		assert.Equal(t, "does.not.exist", b.T("en", "does.not.exist", nil))
	})
}
