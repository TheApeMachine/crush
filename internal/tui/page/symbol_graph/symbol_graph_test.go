package symbolgraph

import (
	"context"
	"testing"

	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	// Create a minimal app for testing
	cfg := &config.Config{}
	cfg.Providers = csync.NewMap[string, config.ProviderConfig]()
	// Use in-memory DB so page can query storage without hitting nil
	testDB, err := db.ConnectInMemoryForTest(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = testDB.Close() })

	app, err := app.New(context.Background(), testDB, cfg)
	require.NoError(t, err)

	sgPage := New(app)
	assert.NotNil(t, sgPage)
	// Ensure exported PageID has the expected value
	assert.Equal(t, "symbol_graph", string(SymbolGraphPageID))
}

func TestKeyMap(t *testing.T) {
	keyMap := DefaultKeyMap()
	assert.NotEmpty(t, keyMap.Quit.Keys())
	assert.NotEmpty(t, keyMap.ToggleFocus.Keys())
	assert.NotEmpty(t, keyMap.Search.Keys())
	assert.NotEmpty(t, keyMap.Select.Keys())
}
