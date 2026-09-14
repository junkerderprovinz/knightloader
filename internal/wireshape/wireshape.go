// Package wireshape reads the JSON shape this program promises, off the Go
// structs that produce it.
//
// WHAT IT IS FOR. `omitempty` has never done anything to a struct field, and
// time.Time is a struct. So a time.Time tagged json:"finishedAt,omitempty"
// reads like "absent when nobody set it" and means "always present, and the
// year one when nobody set it": the field ships as the string
// "0001-01-01T00:00:00Z", which is not empty and is therefore TRUE to every
// reader that tests it.
//
// That has already cost this program two shipped bugs on the other side of the
// wire, both of them a sentence on screen that was simply not true - a "Standing
// still 34" chip over a queue in which nothing had ever moved, and a retry glyph
// telling five dead downloads they were about to be tried again. web/check-go-
// timestamps.mjs is the guard on the TypeScript half: it refuses to let any
// timestamp be read as a yes/no, whatever tag the Go struct carries.
//
// This is the Go half, and it is a different question. The .mjs guard makes the
// READER safe; this one is about the tag itself being a claim that is not true.
// A field tagged `omitempty` is a field somebody believed disappears when it is
// not set, and the belief is what the next `if (report.finishedAt)` is built on.
//
// THE FIX IS `omitzero`, which landed in Go 1.24 and does exactly what the
// author of `omitempty` meant: it drops a field whose value is the zero one, and
// it knows a struct's zero value. internal/selftest, internal/notify,
// internal/api's feed and event-target rows and internal/startupcheck all use it.
package wireshape

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// A Field is one struct field that claims something about itself.
type Field struct {
	// File is relative to the root that was scanned, with forward slashes, so a
	// report reads the same on every machine.
	File string
	// Type is the struct that holds it, and Name the Go field name. A field
	// inside an inline anonymous struct is named "Outer.Inner".
	Type string
	Name string
	// JSON is the name it goes out under.
	JSON string
	// Line is for a person going to look, and deliberately not part of the
	// identity: line numbers move, and a guard keyed on one is a guard that
	// fails the next time somebody adds a comment. See Key.
	Line int
}

// Key is the identity of a field, with no line number in it.
func (f Field) Key() string { return f.File + " " + f.Type + "." + f.Name }

// ZeroTimeOmitempty is every non-pointer time.Time in the tree under root whose
// json tag carries `omitempty` - that is, every field whose tag promises it can
// be absent and which is in fact always sent.
//
// POINTERS ARE NOT IN THE ANSWER and that is the whole distinction: `*time.Time`
// with `omitempty` works perfectly, because a nil pointer IS empty to
// encoding/json. Five fields in this tree do it that way and none of them is a
// problem.
//
// _test.go is skipped. A struct declared inside a test is a fixture written to
// exercise a decoder, not a shape this program promises anybody.
func ZeroTimeOmitempty(root string) ([]Field, error) {
	var out []Field
	skip := map[string]bool{".git": true, "node_modules": true, "web": true, "dist": true, "testdata": true, "mobile": true}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return perr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				out = append(out, walkStruct(fset, rel, ts.Name.Name, st)...)
			}
		}
		return nil
	})
	return out, err
}

// walkStruct reports this struct's own offenders and those of any struct
// declared inline inside it.
func walkStruct(fset *token.FileSet, file, typeName string, st *ast.StructType) []Field {
	var out []Field
	for _, f := range st.Fields.List {
		if inner, ok := f.Type.(*ast.StructType); ok && len(f.Names) == 1 {
			out = append(out, walkStruct(fset, file, typeName+"."+f.Names[0].Name, inner)...)
			continue
		}
		if !isTimeTime(f.Type) || f.Tag == nil || len(f.Names) != 1 {
			continue
		}
		tag, err := strconv.Unquote(f.Tag.Value)
		if err != nil {
			continue
		}
		name, opts, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
		if name == "-" || !hasOption(opts, "omitempty") {
			continue
		}
		out = append(out, Field{
			File: file,
			Type: typeName,
			Name: f.Names[0].Name,
			JSON: name,
			Line: fset.Position(f.Pos()).Line,
		})
	}
	return out
}

// isTimeTime is true for `time.Time` and false for `*time.Time`, which is the
// point of the whole check.
func isTimeTime(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Time" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "time"
}

func hasOption(opts, want string) bool {
	for _, o := range strings.Split(opts, ",") {
		if o == want {
			return true
		}
	}
	return false
}
