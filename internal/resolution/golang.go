package resolution

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"

	"github.com/charmbracelet/crush/internal/treesitter"
	"golang.org/x/tools/go/packages"
)

// ResolveCallsForFile analyzes a Go source file within the workspace and returns exact call relationships
// where both callee and caller are resolved to unique definitions using go/types.
// Returned relationships have From/To set to symbol IDs (name:line:column) compatible with the DB schema.
func ResolveCallsForFile(ctx context.Context, workspaceRoot, relFilename string) ([]treesitter.Relationship, error) {
	absFile := filepath.Join(workspaceRoot, relFilename)

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedModule,
		Dir:  workspaceRoot,
		Env:  nil,
	}

	// Load all packages in the workspace to allow cross-package resolution
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}

	var rels []treesitter.Relationship

	for _, pkg := range pkgs {
		if pkg == nil || pkg.Fset == nil || pkg.TypesInfo == nil {
			continue
		}
		for i, f := range pkg.Syntax {
			if f == nil {
				continue
			}
			// Only analyze the target file
			filename := pkg.CompiledGoFiles[i]
			if !sameFile(filename, absFile) {
				continue
			}

			// Track current function/method for From
			var currentFn *ast.FuncDecl
			ast.Inspect(f, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.FuncDecl:
					currentFn = node
					return true
				case *ast.CallExpr:
					if currentFn == nil {
						return true
					}
					fromID := extractFuncSymbolID(pkg.Fset, currentFn)
					if fromID == "" {
						return true
					}

					toID := resolveCallCalleeSymbolID(pkg, node)
					if toID == "" {
						return true
					}

					pos := pkg.Fset.Position(node.Lparen)
					if !pos.IsValid() {
						pos = pkg.Fset.Position(node.Pos())
					}
					rels = append(rels, treesitter.Relationship{
						From: fromID,
						To:   toID,
						Type: treesitter.RelationshipTypeCall,
						Position: treesitter.Position{
							Line:   uint32(pos.Line),
							Column: uint32(pos.Column),
						},
					})
					return true
				default:
					return true
				}
			})
		}
	}

	return rels, nil
}

func extractFuncSymbolID(fset *token.FileSet, decl *ast.FuncDecl) string {
	if decl == nil || decl.Name == nil {
		return ""
	}
	pos := fset.Position(decl.Name.Pos())
	if !pos.IsValid() {
		return ""
	}
	name := decl.Name.Name
	return treesitter.GenerateSymbolID(name, treesitter.Position{Line: uint32(pos.Line), Column: uint32(pos.Column)})
}

func resolveCallCalleeSymbolID(pkg *packages.Package, call *ast.CallExpr) string {
	if pkg == nil || call == nil || pkg.TypesInfo == nil {
		return ""
	}

	var obj types.Object
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		obj = pkg.TypesInfo.Uses[fun]
	case *ast.SelectorExpr:
		// Method or selector; prefer selections map for methods
		if sel := pkg.TypesInfo.Selections[fun]; sel != nil {
			obj = sel.Obj()
		} else {
			id := fun.Sel
			obj = pkg.TypesInfo.Uses[id]
		}
	default:
		// Unsupported callee expression (e.g., func literal); skip
		return ""
	}

	if obj == nil {
		return ""
	}

	// Only functions and methods
	fn, ok := obj.(*types.Func)
	if !ok {
		return ""
	}

	pos := pkg.Fset.Position(fn.Pos())
	if !pos.IsValid() {
		return ""
	}

	name := fn.Name()
	return treesitter.GenerateSymbolID(name, treesitter.Position{Line: uint32(pos.Line), Column: uint32(pos.Column)})
}

func sameFile(a, b string) bool {
	ap, _ := filepath.Abs(a)
	bp, _ := filepath.Abs(b)
	return ap == bp
}
