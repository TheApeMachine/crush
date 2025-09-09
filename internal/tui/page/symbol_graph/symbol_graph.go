package symbolgraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/v2/help"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/textinput"
	"github.com/charmbracelet/bubbles/v2/viewport"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/treesitter"
	"github.com/charmbracelet/crush/internal/tui/components/core"
	"github.com/charmbracelet/crush/internal/tui/components/core/layout"
	"github.com/charmbracelet/crush/internal/tui/page"
	"github.com/charmbracelet/crush/internal/tui/styles"
	"github.com/charmbracelet/crush/internal/tui/util"
	"github.com/charmbracelet/lipgloss/v2"
)

var SymbolGraphPageID page.PageID = "symbol_graph"

type (
	SymbolSelectedMsg struct {
		Symbol *treesitter.Symbol
	}
	FileSelectedMsg struct {
		FilePath string
	}
)

// PanelType represents different panels in the symbol graph page
type PanelType string

const (
	PanelTypeGraph   PanelType = "graph"
	PanelTypeDetails PanelType = "details"
	PanelTypeSearch  PanelType = "search"
	PanelTypeFiles   PanelType = "files"
)

// Layout constants
const (
	GraphPanelWidthRatio   = 0.7
	DetailsPanelWidthRatio = 0.3
	SearchHeight           = 3
	FileTreeWidthRatio     = 0.2
	MinWidth               = 80
	MinHeight              = 20
	NodeSpacing            = 2
	MaxVisibleNodes        = 50
)

// SymbolGraphPage interface
type SymbolGraphPage interface {
	util.Model
	layout.Help
	LoadFile(filePath string) tea.Cmd
	GetSelectedSymbol() *treesitter.Symbol
}

// symbolGraphPage implements the symbol graph page
type symbolGraphPage struct {
	width, height        int
	app                  *app.App
	keyboardEnhancements tea.KeyboardEnhancementsMsg

	// Layout state
	focusedPanel PanelType
	compact      bool
	unified      bool // when true, show the unified codebase graph; otherwise per-file
	showEdges    bool // when true, list outgoing edges under each symbol
	graphMode    bool // when true, render ASCII node/edge graph

	// Data
	currentFile     string
	symbolGraph     *treesitter.SymbolGraph
	selectedSymbol  *treesitter.Symbol
	filteredSymbols []*treesitter.Symbol
	searchQuery     string
	focusSymbolID   string   // center of graph mode
	graphDepth      int      // BFS depth for graph mode
	graphVisible    []string // ordered list of visible node IDs for selection

	// Components
	graphViewport     viewport.Model
	detailsViewport   viewport.Model
	searchInput       textinput.Model
	fileList          []string
	selectedFileIndex int

	// Cached panel size for graph rendering
	graphW int
	graphH int

	// Key map
	keyMap KeyMap
}

// New creates a new symbol graph page
func New(app *app.App) SymbolGraphPage {
	searchInput := textinput.New()
	searchInput.Placeholder = "Search symbols..."
	searchInput.CharLimit = 100

	graphVp := viewport.New()
	detailsVp := viewport.New()

	return &symbolGraphPage{
		app:             app,
		keyMap:          DefaultKeyMap(),
		graphViewport:   graphVp,
		detailsViewport: detailsVp,
		searchInput:     searchInput,
		focusedPanel:    PanelTypeGraph,
		filteredSymbols: []*treesitter.Symbol{},
		fileList:        []string{},
		unified:         true,
		showEdges:       true,
		graphMode:       false,
		graphDepth:      1,
	}
}

// Init initializes the page
func (p *symbolGraphPage) Init() tea.Cmd {
	p.searchInput.Focus()
	return tea.Batch(
		p.loadFileList(),
		p.SetSize(p.width, p.height),
		p.LoadUnifiedGraph(),
	)
}

// Update handles messages
func (p *symbolGraphPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyboardEnhancementsMsg:
		p.keyboardEnhancements = msg
		return p, nil
	case tea.WindowSizeMsg:
		return p, p.SetSize(msg.Width, msg.Height)
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, p.keyMap.Quit):
			// Navigate back to main chat page instead of quitting
			return p, util.CmdHandler(page.PageChangeMsg{ID: "chat"})
		case key.Matches(msg, p.keyMap.ToggleFocus):
			p.toggleFocus()
			return p, nil
		case key.Matches(msg, p.keyMap.Search):
			p.focusedPanel = PanelTypeSearch
			p.searchInput.Focus()
			return p, nil
		case key.Matches(msg, p.keyMap.Select):
			if p.focusedPanel == PanelTypeGraph {
				if p.graphMode {
					// In graph mode, Enter expands: set focus to current selection
					if p.selectedSymbol != nil {
						p.focusSymbolID = p.selectedSymbol.ID
						p.updateGraphView()
					}
					return p, nil
				}
				if len(p.filteredSymbols) > 0 {
					p.selectCurrentSymbol()
					return p, nil
				}
			}
		case key.Matches(msg, p.keyMap.PanUp):
			if p.focusedPanel == PanelTypeGraph {
				if p.graphMode {
					p.navigateGraphVertical(-1)
				} else {
					p.navigateSymbols(-1)
				}
				return p, nil
			}
		case key.Matches(msg, p.keyMap.PanDown):
			if p.focusedPanel == PanelTypeGraph {
				if p.graphMode {
					p.navigateGraphVertical(1)
				} else {
					p.navigateSymbols(1)
				}
				return p, nil
			}
		case key.Matches(msg, p.keyMap.PanLeft):
			if p.focusedPanel == PanelTypeGraph && p.graphMode {
				p.navigateGraphHorizontal(-1)
				return p, nil
			}
		case key.Matches(msg, p.keyMap.PanRight):
			if p.focusedPanel == PanelTypeGraph && p.graphMode {
				p.navigateGraphHorizontal(1)
				return p, nil
			}
		case key.Matches(msg, p.keyMap.ZoomIn):
			if p.graphMode {
				if p.graphDepth < 3 {
					p.graphDepth++
					p.updateGraphView()
				}
				return p, nil
			}
		case key.Matches(msg, p.keyMap.ZoomOut):
			if p.graphMode {
				if p.graphDepth > 1 {
					p.graphDepth--
					p.updateGraphView()
				}
				return p, nil
			}
		case key.Matches(msg, p.keyMap.Refresh):
			// Reload file list and graph
			return p, tea.Sequence(p.loadFileList(), func() tea.Msg {
				if p.unified {
					return p.LoadUnifiedGraph()()
				}
				if p.currentFile != "" {
					return p.LoadFile(p.currentFile)()
				}
				return nil
			})
		case key.Matches(msg, p.keyMap.ToggleEdges):
			p.showEdges = !p.showEdges
			p.updateGraphView()
			return p, nil
		case key.Matches(msg, p.keyMap.ToggleGraph):
			p.graphMode = !p.graphMode
			if p.graphMode {
				// Initialize focus
				if p.selectedSymbol == nil || !p.hasRelations(p.selectedSymbol.ID) {
					p.autoSelectSymbolWithRelationships()
				}
				if p.selectedSymbol != nil {
					p.focusSymbolID = p.selectedSymbol.ID
				} else {
					p.focusSymbolID = ""
				}
			}
			p.updateGraphView()
			return p, nil
		}
	}

	// Handle component updates based on focused panel
	switch p.focusedPanel {
	case PanelTypeSearch:
		if p.searchInput.Focused() {
			var cmd tea.Cmd
			p.searchInput, cmd = p.searchInput.Update(msg)
			cmds = append(cmds, cmd)

			// Update search results
			p.searchQuery = p.searchInput.Value()
			p.updateFilteredSymbols()
			p.updateGraphView()
		}
	case PanelTypeGraph:
		var cmd tea.Cmd
		p.graphViewport, cmd = p.graphViewport.Update(msg)
		cmds = append(cmds, cmd)
	case PanelTypeDetails:
		var cmd tea.Cmd
		p.detailsViewport, cmd = p.detailsViewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return p, tea.Batch(cmds...)
}

// View renders the page
func (p *symbolGraphPage) View() string {
	if p.width < MinWidth || p.height < MinHeight {
		return "Terminal too small. Please resize to at least 80x20."
	}

	t := styles.CurrentTheme()

	// Search bar
	searchView := p.renderSearchBar(t)

	// Main content area
	mainHeight := p.height - SearchHeight
	graphWidth := int(float64(p.width) * GraphPanelWidthRatio)
	detailsWidth := p.width - graphWidth

	// Graph panel
	graphView := p.renderGraphPanel(t, graphWidth, mainHeight)

	// Details panel
	detailsView := p.renderDetailsPanel(t, detailsWidth, mainHeight)

	// Combine panels
	borderStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Border)
	content := lipgloss.JoinHorizontal(
		lipgloss.Left,
		graphView,
		borderStyle.Render(""),
		detailsView,
	)

	// Combine search and content
	return lipgloss.JoinVertical(
		lipgloss.Left,
		searchView,
		borderStyle.Render(""),
		content,
	)
}

// renderSearchBar renders the search input bar
func (p *symbolGraphPage) renderSearchBar(t *styles.Theme) string {
	searchStyle := t.S().Base.Width(p.width).Height(SearchHeight)
	if p.focusedPanel == PanelTypeSearch {
		searchStyle = searchStyle.Border(lipgloss.RoundedBorder()).BorderForeground(t.BorderFocus)
	}

	searchContent := lipgloss.JoinVertical(
		lipgloss.Left,
		t.S().Muted.Render("Search:"),
		p.searchInput.View(),
	)

	return searchStyle.Render(searchContent)
}

// renderGraphPanel renders the graph visualization
func (p *symbolGraphPage) renderGraphPanel(t *styles.Theme, width, height int) string {
	p.graphViewport.SetWidth(width)
	p.graphViewport.SetHeight(height)
	// cache for renderer
	p.graphW = width
	p.graphH = height

	style := t.S().Base.Width(width).Height(height)
	if p.focusedPanel == PanelTypeGraph {
		style = style.Border(lipgloss.RoundedBorder()).BorderForeground(t.BorderFocus)
	}

	// If no symbol graph or empty content, show a helpful hint
	if p.symbolGraph == nil || len(p.symbolGraph.Symbols) == 0 {
		p.graphViewport.SetContent("No symbols indexed yet. The indexer runs at startup. If this remains empty, ensure the workspace was scanned and contains supported files.")
	}

	return style.Render(p.graphViewport.View())
}

// renderDetailsPanel renders the symbol details
func (p *symbolGraphPage) renderDetailsPanel(t *styles.Theme, width, height int) string {
	p.detailsViewport.SetWidth(width)
	p.detailsViewport.SetHeight(height)

	style := t.S().Base.Width(width).Height(height)
	if p.focusedPanel == PanelTypeDetails {
		style = style.Border(lipgloss.RoundedBorder()).BorderForeground(t.BorderFocus)
	}

	return style.Render(p.detailsViewport.View())
}

// updateFilteredSymbols filters symbols based on search query
func (p *symbolGraphPage) updateFilteredSymbols() {
	if p.symbolGraph == nil {
		p.filteredSymbols = []*treesitter.Symbol{}
		return
	}

	if p.searchQuery == "" {
		// Convert map values to slice
		p.filteredSymbols = make([]*treesitter.Symbol, 0, len(p.symbolGraph.Symbols))
		for _, symbol := range p.symbolGraph.Symbols {
			p.filteredSymbols = append(p.filteredSymbols, symbol)
		}
		return
	}

	p.filteredSymbols = []*treesitter.Symbol{}
	query := strings.ToLower(p.searchQuery)

	for _, symbol := range p.symbolGraph.Symbols {
		if strings.Contains(strings.ToLower(symbol.Name), query) ||
			strings.Contains(strings.ToLower(string(symbol.Type)), query) {
			p.filteredSymbols = append(p.filteredSymbols, symbol)
		}
	}
}

// updateGraphView updates the graph viewport content
func (p *symbolGraphPage) updateGraphView() {
	if len(p.filteredSymbols) == 0 {
		p.graphViewport.SetContent("No symbols found.\n\nUse '/' to search for symbols.")
		return
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("Symbol Graph (%d symbols)%s\n", len(p.filteredSymbols), func() string {
		if p.graphMode {
			return " [graph]"
		}
		return ""
	}()))
	content.WriteString(strings.Repeat("=", 40) + "\n\n")
	// Legend / controls
	content.WriteString("Legend: → call, r: refresh, e: toggle edges, g: graph mode, Enter: expand, +/- depth\n\n")

	if p.graphMode {
		// Render a BFS neighborhood around focus with depth control
		p.renderAsciiGraph(&content)
		p.graphViewport.SetContent(content.String())
		return
	}

	// Display symbols with their relationships (list mode)
	for i, symbol := range p.filteredSymbols {
		if i >= MaxVisibleNodes {
			content.WriteString(fmt.Sprintf("... and %d more symbols\n", len(p.filteredSymbols)-MaxVisibleNodes))
			break
		}

		selected := symbol == p.selectedSymbol
		prefix := "  "
		if selected {
			prefix = "▶ "
		}

		symbolType := string(symbol.Type)
		content.WriteString(fmt.Sprintf("%s[%s] %s", prefix, symbolType, symbol.Name))

		// Show position
		content.WriteString(fmt.Sprintf(" (line %d)", symbol.Position.Line))

		// Show relationships
		if p.symbolGraph != nil {
			relationships := p.symbolGraph.GetRelationships(symbol.ID)
			if len(relationships) > 0 {
				content.WriteString(fmt.Sprintf(" → %d relationships", len(relationships)))
			}
		}

		content.WriteString("\n")

		// Show a few relationships
		if p.symbolGraph != nil && (selected || p.showEdges) {
			relationships := p.symbolGraph.GetRelationships(symbol.ID)
			for j, rel := range relationships {
				if j >= 3 { // Show only first 3 relationships
					if len(relationships) > 3 {
						content.WriteString(fmt.Sprintf("      ... and %d more\n", len(relationships)-3))
					}
					break
				}

				if targetSymbol, exists := p.symbolGraph.Symbols[rel.To]; exists {
					relType := string(rel.Type)
					content.WriteString(fmt.Sprintf("      %s → %s\n", relType, targetSymbol.Name))
				}
			}
			if len(relationships) > 0 {
				content.WriteString("\n")
			}
		}
	}

	// Show navigation hint
	content.WriteString("\nUse ↑↓ to navigate, Enter to select, Tab to switch panels")

	p.graphViewport.SetContent(content.String())
}

// renderAsciiGraph renders nodes and edges around focusSymbolID
func (p *symbolGraphPage) renderAsciiGraph(b *strings.Builder) {
	if p.symbolGraph == nil || p.focusSymbolID == "" {
		b.WriteString("No focus symbol. Use search to select a symbol, then press Enter.\n")
		return
	}
	focus := p.symbolGraph.Symbols[p.focusSymbolID]
	if focus == nil {
		b.WriteString("Focus symbol not found.\n")
		return
	}

	// Build bidirectional layered layout up to depth
	leftLevels, rightLevels := p.buildGraphSides(focus.ID, p.graphDepth)

	// Prepare canvas
	width := p.graphW
	height := p.graphH
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 20
	}
	canvas := make([][]rune, height)
	for i := range canvas {
		canvas[i] = make([]rune, width)
		for j := 0; j < width; j++ {
			canvas[i][j] = ' '
		}
	}

	// Node box metrics
	boxW := 24
	boxH := 3
	colW := boxW + 12
	// Positions map
	type nodePos struct{ x, y int }
	positions := make(map[string]nodePos)

	// Helper to draw box
	drawBox := func(x, y int, name string, selected bool) {
		if x < 0 || y < 0 || x+boxW >= width || y+boxH >= height {
			return
		}
		h := '─'
		v := '│'
		tl := '┌'
		tr := '┐'
		bl := '└'
		br := '┘'
		if selected {
			h = '═'
			v = '║'
			tl = '╔'
			tr = '╗'
			bl = '╚'
			br = '╝'
		}
		// top
		canvas[y][x] = tl
		for i := 1; i < boxW-1; i++ {
			canvas[y][x+i] = h
		}
		canvas[y][x+boxW-1] = tr
		// middle
		canvas[y+1][x] = v
		// label
		label := name
		if len([]rune(label)) > boxW-2 {
			label = string([]rune(label)[:boxW-5]) + "…"
		}
		rs := []rune(label)
		for i := 0; i < boxW-2; i++ {
			ch := ' '
			if i < len(rs) {
				ch = rs[i]
			}
			canvas[y+1][x+1+i] = ch
		}
		canvas[y+1][x+boxW-1] = v
		// bottom
		canvas[y+2][x] = bl
		for i := 1; i < boxW-1; i++ {
			canvas[y+2][x+i] = h
		}
		canvas[y+2][x+boxW-1] = br
	}

	// Compute total columns: left (reversed), center, right
	leftCols := len(leftLevels)
	rightCols := len(rightLevels)
	_ = rightCols // kept for clarity; used below

	// Place center
	centerColX := 2 + leftCols*colW
	// vertical spacing for each column computed per column

	// Place left side (incoming) from center-1 going left
	for col := 0; col < leftCols; col++ {
		nodes := leftLevels[col]
		if len(nodes) == 0 {
			continue
		}
		gap := height / (len(nodes) + 1)
		if gap < boxH+1 {
			gap = boxH + 1
		}
		x := centerColX - (col+1)*colW
		for i, id := range nodes {
			y := 1 + i*gap
			positions[id] = nodePos{x: x, y: y}
			name := p.symbolGraph.Symbols[id].Name
			selected := p.selectedSymbol != nil && p.selectedSymbol.ID == id
			drawBox(x, y, name, selected)
		}
	}

	// Place center focus
	{
		nodes := []string{focus.ID}
		gap := height / (len(nodes) + 1)
		if gap < boxH+1 {
			gap = boxH + 1
		}
		x := centerColX
		for i, id := range nodes {
			y := 1 + i*gap
			positions[id] = nodePos{x: x, y: y}
			name := p.symbolGraph.Symbols[id].Name
			selected := p.selectedSymbol != nil && p.selectedSymbol.ID == id
			drawBox(x, y, name, selected)
		}
	}

	// Place right side (outgoing) from center+1 going right
	for col := 0; col < rightCols; col++ {
		nodes := rightLevels[col]
		if len(nodes) == 0 {
			continue
		}
		gap := height / (len(nodes) + 1)
		if gap < boxH+1 {
			gap = boxH + 1
		}
		x := centerColX + (col+1)*colW
		for i, id := range nodes {
			y := 1 + i*gap
			positions[id] = nodePos{x: x, y: y}
			name := p.symbolGraph.Symbols[id].Name
			selected := p.selectedSymbol != nil && p.selectedSymbol.ID == id
			drawBox(x, y, name, selected)
		}
	}

	// Draw routed edges across the visible columns
	set := func(x, y int, r rune) {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if y >= height {
			y = height - 1
		}
		if x >= width {
			x = width - 1
		}
		canvas[y][x] = r
	}
	drawH := func(y, x1, x2 int) {
		if y < 0 {
			y = 0
		}
		if y >= height {
			y = height - 1
		}
		if x1 > x2 {
			x1, x2 = x2, x1
		}
		if x2 < 0 || x1 >= width {
			return
		}
		if x1 < 0 {
			x1 = 0
		}
		if x2 >= width {
			x2 = width - 1
		}
		for x := x1; x <= x2; x++ {
			if canvas[y][x] == ' ' {
				set(x, y, '─')
			} else {
				set(x, y, '┼')
			}
		}
	}
	drawV := func(x, y1, y2 int) {
		if x < 0 {
			x = 0
		}
		if x >= width {
			x = width - 1
		}
		if y1 > y2 {
			y1, y2 = y2, y1
		}
		if y2 < 0 || y1 >= height {
			return
		}
		if y1 < 0 {
			y1 = 0
		}
		if y2 >= height {
			y2 = height - 1
		}
		for y := y1; y <= y2; y++ {
			if canvas[y][x] == ' ' {
				set(x, y, '│')
			} else {
				set(x, y, '┼')
			}
		}
	}

	// visible set
	visible := make(map[string]bool)
	for _, col := range leftLevels {
		for _, id := range col {
			visible[id] = true
		}
	}
	visible[focus.ID] = true
	for _, col := range rightLevels {
		for _, id := range col {
			visible[id] = true
		}
	}

	for fromID, rels := range p.symbolGraph.Relationships {
		if !visible[fromID] {
			continue
		}
		for _, r := range rels {
			toID := r.To
			if !visible[toID] {
				continue
			}
			sp, ok1 := positions[fromID]
			tp, ok2 := positions[toID]
			if !ok1 || !ok2 {
				continue
			}
			y1 := sp.y + 1
			y2 := tp.y + 1
			x1 := sp.x + boxW
			x2 := tp.x - 1
			// If destination is to the left, draw leftwards route too
			flipped := false
			if x2 < x1 {
				x1, x2 = tp.x+boxW, sp.x-1
				y1, y2 = tp.y+1, sp.y+1
				flipped = true
			}
			mid := (x1 + x2) / 2
			drawH(y1, x1, mid)
			drawV(mid, y1, y2)
			drawH(y2, mid, x2)
			// arrow head (clamped)
			if !flipped {
				set(x2, y2, '>')
			} else {
				set(x1-(x1-mid), y1, '<')
			}
		}
	}

	// Update visible order for navigation: left to right, top to bottom, centered on focus first
	p.graphVisible = p.graphVisible[:0]
	p.graphVisible = append(p.graphVisible, focus.ID)
	for _, nodes := range rightLevels {
		for _, id := range nodes {
			p.graphVisible = append(p.graphVisible, id)
		}
	}
	for _, nodes := range leftLevels {
		for _, id := range nodes {
			p.graphVisible = append(p.graphVisible, id)
		}
	}

	// Write canvas
	for y := 0; y < height; y++ {
		b.WriteString(string(canvas[y]))
		b.WriteString("\n")
	}
	b.WriteString("\nUse arrows/J/K to move (centers on selection), Enter to focus, +/- depth.\n")
}

// buildGraphLevels performs a BFS from focus up to depth and returns node IDs per level
func (p *symbolGraphPage) buildGraphLevels(focusID string, depth int) [][]string {
	if depth < 1 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	levels := make([][]string, depth+1)
	visited := make(map[string]bool)
	queue := []string{focusID}
	levelIdx := map[string]int{focusID: 0}
	visited[focusID] = true
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		l := levelIdx[id]
		if l > depth {
			continue
		}
		levels[l] = append(levels[l], id)
		if l == depth {
			continue
		}
		// neighbors (outgoing and incoming)
		for _, rel := range p.symbolGraph.GetRelationships(id) {
			if !visited[rel.To] {
				visited[rel.To] = true
				levelIdx[rel.To] = l + 1
				queue = append(queue, rel.To)
			}
		}
		for otherID := range p.symbolGraph.Symbols {
			for _, rel := range p.symbolGraph.GetRelationships(otherID) {
				if rel.To == id && !visited[otherID] {
					visited[otherID] = true
					levelIdx[otherID] = l + 1
					queue = append(queue, otherID)
				}
			}
		}
	}
	return levels
}

// buildGraphSides builds two-sided levels: incoming on the left, outgoing on the right
func (p *symbolGraphPage) buildGraphSides(focusID string, depth int) ([][]string, [][]string) {
	if depth < 1 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	// Outgoing (right)
	right := make([][]string, depth)
	visitedR := map[string]bool{focusID: true}
	frontier := []string{focusID}
	for d := 0; d < depth; d++ {
		var next []string
		uniq := make(map[string]bool)
		for _, id := range frontier {
			for _, rel := range p.symbolGraph.GetRelationships(id) {
				to := rel.To
				if !visitedR[to] {
					visitedR[to] = true
					if !uniq[to] {
						uniq[to] = true
						right[d] = append(right[d], to)
						next = append(next, to)
					}
				}
			}
		}
		frontier = next
	}

	// Incoming (left): traverse edges reversed
	left := make([][]string, depth)
	visitedL := map[string]bool{focusID: true}
	frontier = []string{focusID}
	for d := 0; d < depth; d++ {
		var next []string
		uniq := make(map[string]bool)
		for _, id := range frontier {
			for otherID := range p.symbolGraph.Symbols {
				for _, rel := range p.symbolGraph.GetRelationships(otherID) {
					if rel.To == id && !visitedL[otherID] {
						visitedL[otherID] = true
						if !uniq[otherID] {
							uniq[otherID] = true
							left[d] = append(left[d], otherID)
							next = append(next, otherID)
						}
					}
				}
			}
		}
		frontier = next
	}

	return left, right
}

// navigateGraphSelection moves selection over visible nodes in graph mode
func (p *symbolGraphPage) navigateGraphSelection(direction int) {
	if len(p.graphVisible) == 0 {
		return
	}
	// Ensure selectedSymbol is within visible set
	curIdx := -1
	if p.selectedSymbol == nil {
		// default to focus
		for i, id := range p.graphVisible {
			if id == p.focusSymbolID {
				curIdx = i
				break
			}
		}
		if curIdx == -1 {
			curIdx = 0
		}
	} else {
		for i, id := range p.graphVisible {
			if id == p.selectedSymbol.ID {
				curIdx = i
				break
			}
		}
		if curIdx == -1 {
			curIdx = 0
		}
	}
	newIdx := curIdx + direction
	if newIdx < 0 {
		newIdx = len(p.graphVisible) - 1
	} else if newIdx >= len(p.graphVisible) {
		newIdx = 0
	}
	if sym, ok := p.symbolGraph.Symbols[p.graphVisible[newIdx]]; ok {
		p.selectedSymbol = sym
		// Only move highlight; Enter will center on selection
		p.updateGraphView()
		p.updateDetailsView()
	}
}

// navigateGraphHorizontal moves selection to nearest node in the horizontal direction (-1 left, +1 right)
func (p *symbolGraphPage) navigateGraphHorizontal(dir int) {
	if p.symbolGraph == nil || p.selectedSymbol == nil {
		return
	}
	// Build column-wise layout and prefer the nearest column in the given direction,
	// then the smallest vertical delta within that column.
	type nodePos struct {
		id   string
		x, y int
		col  int // negative=left, 0=center, positive=right
	}
	positions := make(map[string]nodePos)
	cols := make(map[int][]nodePos)
	// Rebuild positions similar to renderer
	leftLevels, rightLevels := p.buildGraphSides(p.focusSymbolID, p.graphDepth)
	boxW := 24
	colW := boxW + 12
	leftCols := len(leftLevels)
	centerColX := 2 + leftCols*colW
	height := p.graphH
	if height <= 0 {
		height = 20
	}
	addColumn := func(colIndex, colX int, ids []string) {
		if len(ids) == 0 {
			return
		}
		gap := height / (len(ids) + 1)
		if gap < 4 {
			gap = 4
		}
		for i, id := range ids {
			y := 1 + i*gap
			n := nodePos{id: id, x: colX, y: y, col: colIndex}
			positions[id] = n
			cols[colIndex] = append(cols[colIndex], n)
		}
	}
	// left columns: -1, -2, ...
	for i, ids := range leftLevels {
		x := centerColX - (i+1)*colW
		addColumn(-(i + 1), x, ids)
	}
	// center column: 0
	addColumn(0, centerColX, []string{p.focusSymbolID})
	// right columns: +1, +2, ...
	for i, ids := range rightLevels {
		x := centerColX + (i+1)*colW
		addColumn(i+1, x, ids)
	}

	cur := positions[p.selectedSymbol.ID]
	if dir == 0 {
		return
	}
	step := 1
	if dir < 0 {
		step = -1
	}
	// search nearest column in the intended direction
	for c := cur.col + step; ; c += step {
		candidates, ok := cols[c]
		if !ok {
			// stop if we've moved beyond all known columns
			if (step < 0 && c < -len(leftLevels)) || (step > 0 && c > len(rightLevels)) {
				break
			}
			continue
		}
		// choose by minimal |dy|; tie-break by minimal |dx|
		best := nodePos{}
		bestScore := int(^uint(0) >> 1)
		for _, n := range candidates {
			dy := n.y - cur.y
			if dy < 0 {
				dy = -dy
			}
			dx := n.x - cur.x
			if dx < 0 {
				dx = -dx
			}
			score := dy*10 + dx // prefer vertical proximity heavily
			if score < bestScore {
				bestScore = score
				best = n
			}
		}
		if best.id != "" {
			if sym, ok := p.symbolGraph.Symbols[best.id]; ok {
				p.selectedSymbol = sym
				p.updateGraphView()
				p.updateDetailsView()
			}
			return
		}
	}
}

// navigateGraphVertical moves selection to nearest node vertically (-1 up, +1 down)
func (p *symbolGraphPage) navigateGraphVertical(dir int) {
	if p.symbolGraph == nil || p.selectedSymbol == nil {
		return
	}
	// Build positions similar to renderer
	type nodePos struct {
		id   string
		x, y int
	}
	positions := make(map[string]nodePos)
	leftLevels, rightLevels := p.buildGraphSides(p.focusSymbolID, p.graphDepth)
	boxW := 24
	colW := boxW + 12
	leftCols := len(leftLevels)
	centerColX := 2 + leftCols*colW
	height := p.graphH
	if height <= 0 {
		height = 20
	}
	addColumn := func(colX int, ids []string) {
		if len(ids) == 0 {
			return
		}
		gap := height / (len(ids) + 1)
		if gap < 4 {
			gap = 4
		}
		for i, id := range ids {
			y := 1 + i*gap
			positions[id] = nodePos{id: id, x: colX, y: y}
		}
	}
	// left
	for i, ids := range leftLevels {
		x := centerColX - (i+1)*colW
		addColumn(x, ids)
	}
	// center
	addColumn(centerColX, []string{p.focusSymbolID})
	// right
	for i, ids := range rightLevels {
		x := centerColX + (i+1)*colW
		addColumn(x, ids)
	}

	cur := positions[p.selectedSymbol.ID]
	best := nodePos{}
	bestDist := int(^uint(0) >> 1)
	for _, n := range positions {
		if dir < 0 && n.y >= cur.y {
			continue
		}
		if dir > 0 && n.y <= cur.y {
			continue
		}
		dy := n.y - cur.y
		if dy < 0 {
			dy = -dy
		}
		dx := n.x - cur.x
		if dx < 0 {
			dx = -dx
		}
		d := dy*dy + dx*dx
		if d < bestDist {
			bestDist = d
			best = n
		}
	}
	if best.id != "" {
		if sym, ok := p.symbolGraph.Symbols[best.id]; ok {
			p.selectedSymbol = sym
			// Only move highlight; Enter will center on selection
			p.updateGraphView()
			p.updateDetailsView()
		}
	}
}

// updateDetailsView updates the details viewport content
func (p *symbolGraphPage) updateDetailsView() {
	if p.selectedSymbol == nil {
		p.detailsViewport.SetContent("No symbol selected.\n\nUse arrow keys to select a symbol.")
		return
	}

	symbol := p.selectedSymbol
	var content strings.Builder

	content.WriteString("Symbol Details\n")
	content.WriteString("==============\n\n")
	content.WriteString("Name: ")
	content.WriteString(symbol.Name)
	content.WriteString("\n")
	content.WriteString("Type: ")
	content.WriteString(string(symbol.Type))
	content.WriteString("\n")
	content.WriteString("File: ")
	content.WriteString(p.currentFile)
	content.WriteString("\n")
	content.WriteString(fmt.Sprintf("Line: %d, Column: %d\n", symbol.Position.Line, symbol.Position.Column))
	content.WriteString("Scope: ")
	content.WriteString(string(symbol.Scope))
	content.WriteString("\n")
	content.WriteString("Visibility: ")
	content.WriteString(string(symbol.Visibility))
	content.WriteString("\n")

	if symbol.ReturnType != "" {
		content.WriteString("Return Type: ")
		content.WriteString(symbol.ReturnType)
		content.WriteString("\n")
	}

	if len(symbol.Parameters) > 0 {
		content.WriteString("Parameters:\n")
		for _, param := range symbol.Parameters {
			content.WriteString("  - ")
			content.WriteString(param.Name)
			content.WriteString(": ")
			content.WriteString(param.Type)
			content.WriteString("\n")
		}
	}

	if symbol.Parent != nil {
		content.WriteString("Parent: ")
		content.WriteString(symbol.Parent.Name)
		content.WriteString("\n")
	}

	p.detailsViewport.SetContent(content.String())
}

// toggleFocus switches between panels
func (p *symbolGraphPage) toggleFocus() {
	switch p.focusedPanel {
	case PanelTypeGraph:
		p.focusedPanel = PanelTypeDetails
	case PanelTypeDetails:
		p.focusedPanel = PanelTypeSearch
		p.searchInput.Focus()
	case PanelTypeSearch:
		p.focusedPanel = PanelTypeGraph
		p.searchInput.Blur()
	}
}

// LoadFile loads a symbol graph for the given file
func (p *symbolGraphPage) LoadFile(filePath string) tea.Cmd {
	return func() tea.Msg {
		storage := db.NewSymbolGraphStorage(p.app.DB.(*db.Queries))
		graph, err := storage.GetSymbolGraph(context.Background(), filePath)
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("Failed to load symbol graph: %v", err),
			}
		}

		p.currentFile = filePath
		p.symbolGraph = graph
		p.selectedSymbol = nil
		p.updateFilteredSymbols()
		p.autoSelectSymbolWithRelationships()
		p.updateGraphView()
		p.updateDetailsView()

		return FileSelectedMsg{FilePath: filePath}
	}
}

// LoadUnifiedGraph loads the full workspace graph
func (p *symbolGraphPage) LoadUnifiedGraph() tea.Cmd {
	return func() tea.Msg {
		storage := db.NewSymbolGraphStorage(p.app.DB.(*db.Queries))
		graph, err := storage.GetUnifiedGraph(context.Background())
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("Failed to load unified graph: %v", err),
			}
		}
		p.currentFile = ""
		p.symbolGraph = graph
		p.selectedSymbol = nil
		p.updateFilteredSymbols()
		p.autoSelectSymbolWithRelationships()
		p.updateGraphView()
		p.updateDetailsView()
		return FileSelectedMsg{FilePath: "<unified>"}
	}
}

// loadFileList loads the list of files with symbols
func (p *symbolGraphPage) loadFileList() tea.Cmd {
	return func() tea.Msg {
		storage := db.NewSymbolGraphStorage(p.app.DB.(*db.Queries))
		files, err := storage.ListFilesWithSymbols(context.Background())
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("Failed to load file list: %v", err),
			}
		}

		p.fileList = make([]string, len(files))
		for i, file := range files {
			if fileMeta, ok := file.(db.FileMetadatum); ok {
				p.fileList[i] = fileMeta.Path
			}
		}

		// Auto-load the first file if available
		if len(p.fileList) > 0 {
			// Execute the LoadFile command now to return its message
			return p.LoadFile(p.fileList[0])()
		}

		return nil
	}
}

// GetSelectedSymbol returns the currently selected symbol
func (p *symbolGraphPage) GetSelectedSymbol() *treesitter.Symbol {
	return p.selectedSymbol
}

// SetSize sets the size of the page and its components
func (p *symbolGraphPage) SetSize(width, height int) tea.Cmd {
	p.width = width
	p.height = height

	// Update component sizes
	graphWidth := int(float64(width) * GraphPanelWidthRatio)
	graphHeight := height - SearchHeight
	detailsWidth := width - graphWidth
	detailsHeight := height - SearchHeight

	p.graphViewport.SetWidth(graphWidth)
	p.graphViewport.SetHeight(graphHeight)

	p.detailsViewport.SetWidth(detailsWidth)
	p.detailsViewport.SetHeight(detailsHeight)

	p.searchInput.SetWidth(width - 10) // Account for padding

	return nil
}

// Bindings returns the key bindings for this page
func (p *symbolGraphPage) Bindings() []key.Binding {
	return []key.Binding{
		p.keyMap.Quit,
		p.keyMap.ToggleFocus,
		p.keyMap.Search,
		p.keyMap.Select,
		p.keyMap.Refresh,
	}
}

// Help returns the help model for this page
func (p *symbolGraphPage) Help() help.KeyMap {
	var shortList []key.Binding
	var fullList [][]key.Binding

	shortList = append(shortList,
		p.keyMap.Quit,
		p.keyMap.ToggleFocus,
		p.keyMap.Search,
		p.keyMap.Select,
		p.keyMap.Refresh,
	)

	// Group bindings for full help
	fullList = append(fullList, []key.Binding{
		p.keyMap.Quit,
		p.keyMap.ToggleFocus,
	})

	fullList = append(fullList, []key.Binding{
		p.keyMap.Search,
		p.keyMap.Select,
		p.keyMap.Refresh,
	})

	return core.NewSimpleHelp(shortList, fullList)
}

// Cursor returns the cursor position if applicable
func (p *symbolGraphPage) Cursor() *tea.Cursor {
	if p.focusedPanel == PanelTypeSearch && p.searchInput.Focused() {
		return p.searchInput.Cursor()
	}
	return nil
}

// selectCurrentSymbol selects the currently highlighted symbol
func (p *symbolGraphPage) selectCurrentSymbol() {
	if len(p.filteredSymbols) == 0 {
		return
	}

	// Find current selection index
	currentIndex := -1
	for i, symbol := range p.filteredSymbols {
		if symbol == p.selectedSymbol {
			currentIndex = i
			break
		}
	}

	if currentIndex == -1 && len(p.filteredSymbols) > 0 {
		// No selection, select first
		p.selectedSymbol = p.filteredSymbols[0]
	} else if currentIndex >= 0 {
		// Keep current selection
		p.selectedSymbol = p.filteredSymbols[currentIndex]
	}

	p.updateGraphView()
	p.updateDetailsView()
}

// navigateSymbols navigates through the symbol list
func (p *symbolGraphPage) navigateSymbols(direction int) {
	if len(p.filteredSymbols) == 0 {
		return
	}

	// Find current selection index
	currentIndex := -1
	for i, symbol := range p.filteredSymbols {
		if symbol == p.selectedSymbol {
			currentIndex = i
			break
		}
	}

	// Calculate new index
	newIndex := currentIndex + direction
	if newIndex < 0 {
		newIndex = len(p.filteredSymbols) - 1
	} else if newIndex >= len(p.filteredSymbols) {
		newIndex = 0
	}

	// Update selection
	if newIndex >= 0 && newIndex < len(p.filteredSymbols) {
		p.selectedSymbol = p.filteredSymbols[newIndex]
		p.updateGraphView()
		p.updateDetailsView()
	}
}

// autoSelectSymbolWithRelationships picks a symbol that has any relationships
func (p *symbolGraphPage) autoSelectSymbolWithRelationships() {
	if p.symbolGraph == nil {
		return
	}
	// Prefer a symbol with outgoing relationships
	for _, s := range p.symbolGraph.Symbols {
		if len(p.symbolGraph.GetRelationships(s.ID)) > 0 {
			p.selectedSymbol = s
			return
		}
	}
	// Fallback: pick one that appears as a target
	targets := make(map[string]bool)
	for _, s := range p.symbolGraph.Symbols {
		for _, r := range p.symbolGraph.GetRelationships(s.ID) {
			targets[r.To] = true
		}
	}
	for id := range targets {
		if s, ok := p.symbolGraph.Symbols[id]; ok {
			p.selectedSymbol = s
			return
		}
	}
	// Final fallback: first filtered symbol if available
	if p.selectedSymbol == nil && len(p.filteredSymbols) > 0 {
		p.selectedSymbol = p.filteredSymbols[0]
	}
}

// hasRelations checks both outgoing and incoming relations for a symbol
func (p *symbolGraphPage) hasRelations(id string) bool {
	if p.symbolGraph == nil || id == "" {
		return false
	}
	if len(p.symbolGraph.GetRelationships(id)) > 0 {
		return true
	}
	for _, s := range p.symbolGraph.Symbols {
		for _, r := range p.symbolGraph.GetRelationships(s.ID) {
			if r.To == id {
				return true
			}
		}
	}
	return false
}
