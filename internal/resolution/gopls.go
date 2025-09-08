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

// ResolveGoCallsWithLSP resolves Go call sites to exact definitions using gopls via the LSP client.
// This is an alternative to the go/types-based resolver and returns single-target call edges.
func ResolveGoCallsWithLSP(ctx context.Context, client *lsp.Client, workspaceRoot, relFilename string) ([]treesitter.Relationship, error) {
	absFile := filepath.Join(workspaceRoot, relFilename)
	source, err := os.ReadFile(absFile)
	if err != nil {
		return nil, err
	}

	// Ensure the file is open in the LSP
	_ = client.OpenFile(ctx, absFile)

	// Use our Tree-sitter extractor to find candidate function symbols and call positions
	reg := treesitter.NewRegistry()
	_ = reg.RegisterParser(treesitter.LanguageGo)
	_, err = reg.Parse(relFilename, string(source))
	if err != nil {
		return nil, err
	}

	symbols, relationships, err := reg.ExtractSymbols(relFilename, string(source))
	if err != nil {
		return nil, err
	}

	// Map for finding containing function by line
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
	for i := 0; i < len(fnByLine); i++ {
		for j := i + 1; j < len(fnByLine); j++ {
			if fnByLine[j].line < fnByLine[i].line {
				fnByLine[i], fnByLine[j] = fnByLine[j], fnByLine[i]
			}
		}
	}
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

	var rels []treesitter.Relationship
	for _, r := range relationships {
		if r.Type != treesitter.RelationshipTypeCall {
			continue
		}
		// Convert to 0-based
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

		caller := findCaller(r.Position.Line)
		if caller == nil {
			continue
		}

		targetPath, _ := loc.URI.Path()
		tcontent, terr := os.ReadFile(string(targetPath))
		if terr != nil {
			continue
		}
		tlines := strings.Split(string(tcontent), "\n")
		l := int(loc.Range.Start.Line)
		c := int(loc.Range.Start.Character)
		calleeName := ""
		if l >= 0 && l < len(tlines) {
			calleeName = extractWordAt(tlines[l], c)
		}
		if calleeName == "" {
			calleeName = r.To
		}

		fromID := treesitter.GenerateSymbolID(caller.Name, treesitter.Position{Line: caller.Position.Line, Column: caller.Position.Column})
		toID := treesitter.GenerateSymbolID(calleeName, treesitter.Position{Line: uint32(loc.Range.Start.Line + 1), Column: uint32(loc.Range.Start.Character + 1)})

		rels = append(rels, treesitter.Relationship{
			From:     fromID,
			To:       toID,
			Type:     treesitter.RelationshipTypeCall,
			Position: treesitter.Position{Line: r.Position.Line, Column: r.Position.Column},
		})
	}

	return rels, nil
}
