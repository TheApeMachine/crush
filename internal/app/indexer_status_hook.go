package app

import (
	"github.com/charmbracelet/crush/internal/tui/util"
)

// appStatusReporter is a package-level hook the App sets so background services
// like the indexer can surface status messages to the TUI without a hard
// dependency on the tea.Program.
var appStatusReporter func(util.InfoMsg)

// SetIndexerStatusReporter injects a function that delivers status messages to the UI.
func SetIndexerStatusReporter(f func(util.InfoMsg)) {
	appStatusReporter = f
}

// reportStatus sends a status message to the UI if a reporter is configured.
func reportStatus(msg util.InfoMsg) {
	if appStatusReporter != nil {
		appStatusReporter(msg)
	}
}
