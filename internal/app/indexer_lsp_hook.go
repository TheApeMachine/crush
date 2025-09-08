package app

import "github.com/charmbracelet/crush/internal/lsp"

// appLSPGetter is an optional indirection to fetch LSP clients from the App layer.
// The App should set this variable during initialization to enable LSP-backed indexing.
var appLSPGetter func() map[string]*lsp.Client

// SetIndexerLSPGetter allows the App to provide access to live LSP clients for the indexer.
func SetIndexerLSPGetter(getter func() map[string]*lsp.Client) {
	appLSPGetter = getter
}
