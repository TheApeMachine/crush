package symbolgraph

import (
	"context"
	"testing"

	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	// Create a minimal app for testing
	cfg := &config.Config{}
	cfg.Providers = csync.NewMap[string, config.ProviderConfig]()
	app, err := app.New(context.Background(), nil, cfg)
	require.NoError(t, err)

	page := New(app)
	assert.NotNil(t, page)
	assert.Equal(t, SymbolGraphPageID, "symbol_graph")
}

func TestKeyMap(t *testing.T) {
	keyMap := DefaultKeyMap()
	assert.NotEmpty(t, keyMap.Quit.Keys())
	assert.NotEmpty(t, keyMap.ToggleFocus.Keys())
	assert.NotEmpty(t, keyMap.Search.Keys())
	assert.NotEmpty(t, keyMap.Select.Keys())
}
