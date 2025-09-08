package resolution

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/lsp/protocol"
	"github.com/charmbracelet/crush/internal/treesitter"
)

// ResolveTSCallsForFile resolves TypeScript/JavaScript call sites to exact definitions using the active LSP client.
// It returns precise call relationships with From/To set to symbol IDs (name:line:column) consistent with storage.
func ResolveTSCallsForFile(ctx context.Context, client *lsp.Client, workspaceRoot, relFilename string) ([]treesitter.Relationship, error) {
	absFile := filepath.Join(workspaceRoot, relFilename)
	source, err := os.ReadFile(absFile)
	if err != nil {
		return nil, err
	}

	// Ensure the file is open in the LSP for accurate definition results
	_ = client.OpenFile(ctx, absFile)

	// Parse with Tree-sitter (JavaScript grammar also supports TS identifiers for the purpose of call site function tokens)
	reg := treesitter.NewRegistry()
	_ = reg.RegisterParser(treesitter.LanguageJavaScript)
	tree, err := reg.Parse(relFilename, string(source))
	if err != nil || tree == nil {
		return nil, err
	}

	// Traverse and collect relationships
	var rels []treesitter.Relationship

	// Fallback approach: use existing extractor to get symbols and rough call relationships + positions
	symbols, relationships, err := reg.ExtractSymbols(relFilename, string(source))
	if err != nil {
		return nil, err
	}

	// Build a map of line->function symbol for fast containing-function lookup (approximate): pick nearest preceding function on or before line
	type lineFn struct {
		line uint32
		sym  treesitter.Symbol
	}
	var fnByLine []lineFn
	for _, s := range symbols {
		if s.Type == treesitter.SymbolTypeFunction || s.Type == treesitter.SymbolTypeMethod {
			fnByLine = append(fnByLine, lineFn{line: s.Position.Line, sym: s})
		}
	}
	// simple sort by line
	for i := 0; i < len(fnByLine); i++ {
		for j := i + 1; j < len(fnByLine); j++ {
			if fnByLine[j].line < fnByLine[i].line {
				fnByLine[i], fnByLine[j] = fnByLine[j], fnByLine[i]
			}
		}
	}

	// Helper: find containing function symbol for a given line
	findCaller := func(line uint32) *treesitter.Symbol {
		var last *treesitter.Symbol
		for i := 0; i < len(fnByLine); i++ {
			if fnByLine[i].line <= line {
				last = &fnByLine[i].sym
			} else {
				break
			}
		}
		return last
	}

	// For each call relationship extracted (has Position), ask LSP for definition at a best-effort position
	for _, r := range relationships {
		if r.Type != treesitter.RelationshipTypeCall {
			continue
		}
		// Use the recorded position; LSP expects 0-based line/character
		// Convert to 0-based safely
		line0 := int(r.Position.Line) - 1
		if line0 < 0 {
			line0 = 0
		}
		char0 := int(r.Position.Column) - 1
		if char0 < 0 {
			char0 = 0
		}
		defParams := protocol.DefinitionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: protocol.URIFromPath(absFile)},
				Position:     protocol.Position{Line: uint32(line0), Character: uint32(char0)},
			},
		}
		def, err := client.Definition(ctx, defParams)
		if err != nil || def.Value == nil {
			continue
		}
		// Normalize to first location
		var loc protocol.Location
		switch v := def.Value.(type) {
		case protocol.Location:
			loc = v
		case []protocol.Location:
			if len(v) == 0 {
				continue
			}
			loc = v[0]
		default:
			continue
		}

		// Compute caller and callee symbol IDs
		caller := findCaller(r.Position.Line)
		if caller == nil {
			continue
		}

		// Extract callee symbol name from target file path and range start (best-effort)
		targetPath, _ := loc.URI.Path()
		targetFile := string(targetPath)
		tcontent, terr := os.ReadFile(targetFile)
		if terr != nil {
			continue
		}
		tlines := strings.Split(string(tcontent), "\n")
		l := int(loc.Range.Start.Line)
		c := int(loc.Range.Start.Character)
		var calleeName string
		if l >= 0 && l < len(tlines) {
			calleeName = extractWordAt(tlines[l], c)
		}
		if calleeName == "" {
			// fallback to previously extracted rel.To name
			calleeName = r.To
		}

		// Build IDs consistent with storage (name:line:column)
		fromID := treesitter.GenerateSymbolID(caller.Name, treesitter.Position{Line: caller.Position.Line, Column: caller.Position.Column})
		toID := treesitter.GenerateSymbolID(calleeName, treesitter.Position{Line: uint32(loc.Range.Start.Line + 1), Column: uint32(loc.Range.Start.Character + 1)})

		rels = append(rels, treesitter.Relationship{
			From: fromID,
			To:   toID,
			Type: treesitter.RelationshipTypeCall,
			Position: treesitter.Position{
				Line:   uint32(r.Position.Line),
				Column: uint32(r.Position.Column),
			},
		})
	}

	return rels, nil
}

func extractWordAt(line string, col int) string {
	if col < 0 || col > len(line) {
		col = len(line)
	}
	// expand left
	i := col
	for i > 0 && isIdentChar(rune(line[i-1])) {
		i--
	}
	// expand right
	j := col
	for j < len(line) && isIdentChar(rune(line[j])) {
		j++
	}
	if i < j {
		return line[i:j]
	}
	return ""
}

func isIdentChar(r rune) bool {
	if r == '_' || r == '$' || r == '.' {
		return true
	}
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	return false
}
