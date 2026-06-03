// Command check-confidence-reason is a go/analysis vet tool that enforces
// ADR-008: every populated domain.Finding value must carry an explicit
// ConfidenceReason so that --min-confidence filtering stays interpretable.
//
// It replaces the previous awk script (scripts/check_confidence_reason.sh),
// which could only see a ConfidenceReason field written inline in the struct
// literal. This analyzer also accepts the field being set via a post-construction
// assignment (`f := domain.Finding{...}; f.ConfidenceReason = ...`), including
// inside factory functions, by tracking the variable a literal is bound to.
//
// Usage:
//
//	go run ./tools/cmd/check-confidence-reason ./domain/rules/...
package main

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/singlechecker"
)

const (
	findingPkgPath  = "github.com/hatyibei/shingan/domain"
	findingTypeName = "Finding"
	reasonField     = "ConfidenceReason"
)

// Analyzer is the exported analysis.Analyzer so the tool can also be embedded
// in a multichecker if desired.
var Analyzer = &analysis.Analyzer{
	Name: "confidencereason",
	Doc:  "ensures populated domain.Finding values set ConfidenceReason (ADR-008)",
	Run:  run,
}

func main() { singlechecker.Main(Analyzer) }

func run(pass *analysis.Pass) (interface{}, error) {
	// Pass 1: collect variables whose ConfidenceReason is assigned after
	// construction (`x.ConfidenceReason = ...`), and the variable each Finding
	// literal is bound to (`x := domain.Finding{...}` / `var x = ...`).
	satisfied := map[types.Object]bool{}
	litVar := map[*ast.CompositeLit]types.Object{}

	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch stmt := n.(type) {
			case *ast.AssignStmt:
				recordReasonAssignments(pass, stmt, satisfied)
				recordLiteralBindings(pass, stmt.Lhs, stmt.Rhs, litVar)
			case *ast.ValueSpec:
				// `var x = domain.Finding{...}`
				lhs := make([]ast.Expr, len(stmt.Names))
				for i, name := range stmt.Names {
					lhs[i] = name
				}
				recordLiteralBindings(pass, lhs, stmt.Values, litVar)
			}
			return true
		})
	}

	// Pass 2: report populated Finding literals missing ConfidenceReason that
	// are not rescued by a later assignment.
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isFindingLiteral(pass, lit) {
				return true
			}
			populated, hasReason, analyzable := inspectFields(lit)
			if !analyzable || !populated || hasReason {
				return true
			}
			if obj := litVar[lit]; obj != nil && satisfied[obj] {
				return true
			}
			pass.Reportf(lit.Pos(), "domain.Finding literal missing %s (ADR-008): set it inline or via assignment", reasonField)
			return true
		})
	}
	return nil, nil
}

// recordReasonAssignments marks variables that receive `*.ConfidenceReason = ...`.
func recordReasonAssignments(pass *analysis.Pass, stmt *ast.AssignStmt, satisfied map[types.Object]bool) {
	for _, lhs := range stmt.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != reasonField {
			continue
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
			satisfied[obj] = true
		}
	}
}

// recordLiteralBindings maps each Finding composite literal on the RHS to the
// single identifier it is bound to on the LHS (value or &-pointer form).
func recordLiteralBindings(pass *analysis.Pass, lhs, rhs []ast.Expr, litVar map[*ast.CompositeLit]types.Object) {
	if len(lhs) != len(rhs) {
		return
	}
	for i, r := range rhs {
		lit := asCompositeLit(r)
		if lit == nil || !isFindingLiteral(pass, lit) {
			continue
		}
		id, ok := lhs[i].(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
			litVar[lit] = obj
		}
	}
}

// asCompositeLit unwraps `&CompositeLit` and plain `CompositeLit`.
func asCompositeLit(e ast.Expr) *ast.CompositeLit {
	switch v := e.(type) {
	case *ast.CompositeLit:
		return v
	case *ast.UnaryExpr:
		if lit, ok := v.X.(*ast.CompositeLit); ok {
			return lit
		}
	}
	return nil
}

// inspectFields reports whether the literal has any keyed field (populated),
// whether ConfidenceReason is among them, and whether the literal is keyed
// (analyzable). Positional literals are not analyzable by field name.
func inspectFields(lit *ast.CompositeLit) (populated, hasReason, analyzable bool) {
	if len(lit.Elts) == 0 {
		return false, false, true // empty sentinel: nothing to enforce
	}
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			return false, false, false // positional: skip
		}
		populated = true
		if id, ok := kv.Key.(*ast.Ident); ok && id.Name == reasonField {
			hasReason = true
		}
	}
	return populated, hasReason, true
}

// isFindingLiteral reports whether lit constructs a domain.Finding value.
func isFindingLiteral(pass *analysis.Pass, lit *ast.CompositeLit) bool {
	t := pass.TypesInfo.TypeOf(lit)
	if t == nil {
		return false
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Name() == findingTypeName &&
		obj.Pkg() != nil && obj.Pkg().Path() == findingPkgPath
}
