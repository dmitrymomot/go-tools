// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssautil

// This file defines utility functions for constructing programs in SSA form.

import (
	"go/token"

	"honnef.co/go/tools/go/packages"
	"honnef.co/go/tools/go/types"
	"honnef.co/go/tools/ssa"
)

// Packages creates an SSA program for a set of packages loaded from
// source syntax using the golang.org/x/tools/go/packages.Load function.
// It creates and returns an SSA package for each well-typed package in
// the initial list. The resulting list of packages has the same length
// as initial, and contains a nil if SSA could not be constructed for
// the corresponding initial package.
//
// Code for bodies of functions is not built until Build is called
// on the resulting Program.
//
// The mode parameter controls diagnostics and checking during SSA construction.
//
func Packages(initial []*packages.Package, mode ssa.BuilderMode) (*ssa.Program, []*ssa.Package) {
	var fset *token.FileSet
	if len(initial) > 0 {
		fset = initial[0].Fset
	}

	prog := ssa.NewProgram(fset, mode)
	seen := make(map[*packages.Package]*ssa.Package)
	var create func(p *packages.Package) *ssa.Package
	create = func(p *packages.Package) *ssa.Package {
		ssapkg, ok := seen[p]
		if !ok {
			if p.Types == nil || p.IllTyped {
				// not well typed
				seen[p] = nil
				return nil
			}

			ssapkg = prog.CreatePackage(p.Types, p.Syntax, p.TypesInfo, true)
			seen[p] = ssapkg

			for _, imp := range p.Imports {
				create(imp)
			}
		}
		return ssapkg
	}

	var ssapkgs []*ssa.Package
	for _, p := range initial {
		ssapkgs = append(ssapkgs, create(p))
	}
	return prog, ssapkgs
}

// BuildPackage builds an SSA program with IR for a single package.
//
// It populates pkg by type-checking the specified file ASTs.  All
// dependencies are loaded using the importer specified by tc, which
// typically loads compiler export data; SSA code cannot be built for
// those packages.  BuildPackage then constructs an ssa.Program with all
// dependency packages created, and builds and returns the SSA package
// corresponding to pkg.
//
// The caller must have set pkg.Path() to the import path.
//
// The operation fails if there were any type-checking or import errors.
//
// See ../ssa/example_test.go for an example.
//
func BuildPackage(tc *types.Config, fset *token.FileSet, pkg *types.Package, files []*types.File, mode ssa.BuilderMode) (*ssa.Package, *types.Info, error) {
	if fset == nil {
		panic("no token.FileSet")
	}
	if pkg.Path() == "" {
		panic("package has no import path")
	}

	info := &types.Info{
		Types:      make(map[types.Expr]types.TypeAndValue),
		Defs:       make(map[*types.Ident]types.Object),
		Uses:       make(map[*types.Ident]types.Object),
		Implicits:  make(map[types.Node]types.Object),
		Scopes:     make(map[types.Node]*types.Scope),
		Selections: make(map[*types.SelectorExpr]*types.Selection),
	}
	if err := types.NewChecker(tc, fset, pkg, info).Files(files); err != nil {
		return nil, nil, err
	}

	prog := ssa.NewProgram(fset, mode)

	// Create SSA packages for all imports.
	// Order is not significant.
	created := make(map[*types.Package]bool)
	var createAll func(pkgs []*types.Package)
	createAll = func(pkgs []*types.Package) {
		for _, p := range pkgs {
			if !created[p] {
				created[p] = true
				prog.CreatePackage(p, nil, nil, true)
				createAll(p.Imports())
			}
		}
	}
	createAll(pkg.Imports())

	// Create and build the primary package.
	ssapkg := prog.CreatePackage(pkg, files, info, false)
	ssapkg.Build()
	return ssapkg, info, nil
}
