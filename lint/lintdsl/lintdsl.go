// Package lintdsl provides helpers for implementing static analysis
// checks. Dot-importing this package is encouraged.
package lintdsl

import (
	"bytes"
	"fmt"
	"strings"

	"honnef.co/go/tools/go/constant"
	"honnef.co/go/tools/go/printer"
	"honnef.co/go/tools/go/ssa"
	"honnef.co/go/tools/go/token"
	"honnef.co/go/tools/go/types"
	"honnef.co/go/tools/lint"
)

type packager interface {
	Package() *ssa.Package
}

func CallName(call *ssa.CallCommon) string {
	if call.IsInvoke() {
		return ""
	}
	switch v := call.Value.(type) {
	case *ssa.Function:
		fn, ok := v.Object().(*types.Func)
		if !ok {
			return ""
		}
		return fn.FullName()
	case *ssa.Builtin:
		return v.Name()
	}
	return ""
}

func IsCallTo(call *ssa.CallCommon, name string) bool { return CallName(call) == name }
func IsType(T types.Type, name string) bool           { return types.TypeString(T, nil) == name }

func FilterDebug(instr []ssa.Instruction) []ssa.Instruction {
	var out []ssa.Instruction
	for _, ins := range instr {
		if _, ok := ins.(*ssa.DebugRef); !ok {
			out = append(out, ins)
		}
	}
	return out
}

func IsExample(fn *ssa.Function) bool {
	if !strings.HasPrefix(fn.Name(), "Example") {
		return false
	}
	f := fn.Prog.Fset.File(fn.Pos())
	if f == nil {
		return false
	}
	return strings.HasSuffix(f.Name(), "_test.go")
}

func IsPointerLike(T types.Type) bool {
	switch T := T.Underlying().(type) {
	case *types.Interface, *types.Chan, *types.Map, *types.Pointer:
		return true
	case *types.Basic:
		return T.Kind() == types.UnsafePointer
	}
	return false
}

func IsGenerated(f *types.File) bool {
	comments := f.Comments
	if len(comments) > 0 {
		comment := comments[0].Text()
		return strings.Contains(comment, "Code generated by") ||
			strings.Contains(comment, "DO NOT EDIT")
	}
	return false
}

func IsIdent(expr types.Expr, ident string) bool {
	id, ok := expr.(*types.Ident)
	return ok && id.Name == ident
}

// isBlank returns whether id is the blank identifier "_".
// If id == nil, the answer is false.
func IsBlank(id types.Expr) bool {
	ident, _ := id.(*types.Ident)
	return ident != nil && ident.Name == "_"
}

func IsIntLiteral(expr types.Expr, literal string) bool {
	lit, ok := expr.(*types.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == literal
}

// Deprecated: use IsIntLiteral instead
func IsZero(expr types.Expr) bool {
	return IsIntLiteral(expr, "0")
}

func IsOfType(expr types.Expr, name string) bool { return IsType(expr.Type(), name) }

func IsInTest(j *lint.Job, node lint.Positioner) bool {
	// FIXME(dh): this doesn't work for global variables with
	// initializers
	f := j.Program.SSA.Fset.File(node.Pos())
	return f != nil && strings.HasSuffix(f.Name(), "_test.go")
}

func IsInMain(j *lint.Job, node lint.Positioner) bool {
	if node, ok := node.(packager); ok {
		return node.Package().Pkg.Name() == "main"
	}
	pkg := j.NodePackage(node)
	if pkg == nil {
		return false
	}
	return pkg.Types.Name() == "main"
}

func SelectorName(expr *types.SelectorExpr) string {
	sel := expr.Selection
	if sel == nil {
		if x, ok := expr.X.(*types.Ident); ok {
			pkg, ok := x.Obj().(*types.PkgName)
			if !ok {
				// This shouldn't happen
				return fmt.Sprintf("%s.%s", x.Name, expr.Sel.Name)
			}
			return fmt.Sprintf("%s.%s", pkg.Imported().Path(), expr.Sel.Name)
		}
		panic(fmt.Sprintf("unsupported selector: %v", expr))
	}
	return fmt.Sprintf("(%s).%s", sel.Recv(), sel.Obj().Name())
}

func BoolConst(expr types.Expr) bool {
	val := expr.(*types.Ident).Obj().(*types.Const).Val()
	return constant.BoolVal(val)
}

func IsBoolConst(expr types.Expr) bool {
	// We explicitly don't support typed bools because more often than
	// not, custom bool types are used as binary enums and the
	// explicit comparison is desired.

	ident, ok := expr.(*types.Ident)
	if !ok {
		return false
	}
	c, ok := ident.Obj().(*types.Const)
	if !ok {
		return false
	}
	basic, ok := c.Type().(*types.Basic)
	if !ok {
		return false
	}
	if basic.Kind() != types.UntypedBool && basic.Kind() != types.Bool {
		return false
	}
	return true
}

func ExprToInt(expr types.Expr) (int64, bool) {
	tv := expr.TV()
	if tv.Value == nil {
		return 0, false
	}
	if tv.Value.Kind() != constant.Int {
		return 0, false
	}
	return constant.Int64Val(tv.Value)
}

func ExprToString(expr types.Expr) (string, bool) {
	val := expr.TV().Value
	if val == nil {
		return "", false
	}
	if val.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(val), true
}

// Dereference returns a pointer's element type; otherwise it returns
// T.
func Dereference(T types.Type) types.Type {
	if p, ok := T.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return T
}

// DereferenceR returns a pointer's element type; otherwise it returns
// T. If the element type is itself a pointer, DereferenceR will be
// applied recursively.
func DereferenceR(T types.Type) types.Type {
	if p, ok := T.Underlying().(*types.Pointer); ok {
		return DereferenceR(p.Elem())
	}
	return T
}

func IsGoVersion(j *lint.Job, minor int) bool {
	return j.Program.GoVersion >= minor
}

func CallNameAST(call *types.CallExpr) string {
	sel, ok := call.Fun.(*types.SelectorExpr)
	if !ok {
		return ""
	}
	fn, ok := sel.Sel.Obj().(*types.Func)
	if !ok {
		return ""
	}
	return fn.FullName()
}

func IsCallToAST(node types.Node, name string) bool {
	call, ok := node.(*types.CallExpr)
	if !ok {
		return false
	}
	return CallNameAST(call) == name
}

func IsCallToAnyAST(node types.Node, names ...string) bool {
	for _, name := range names {
		if IsCallToAST(node, name) {
			return true
		}
	}
	return false
}

func Render(j *lint.Job, x interface{}) string {
	fset := j.Program.SSA.Fset
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, x); err != nil {
		panic(err)
	}
	return buf.String()
}

func RenderArgs(j *lint.Job, args []types.Expr) string {
	var ss []string
	for _, arg := range args {
		ss = append(ss, Render(j, arg))
	}
	return strings.Join(ss, ", ")
}

func Preamble(f *types.File) string {
	cutoff := f.Package
	if f.Doc != nil {
		cutoff = f.Doc.Pos()
	}
	var out []string
	for _, cmt := range f.Comments {
		if cmt.Pos() >= cutoff {
			break
		}
		out = append(out, cmt.Text())
	}
	return strings.Join(out, "\n")
}

func Inspect(node types.Node, fn func(node types.Node) bool) {
	if node == nil {
		return
	}
	types.Inspect(node, fn)
}
