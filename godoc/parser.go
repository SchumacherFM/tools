// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file contains support functions for parsing .go files
// accessed via godoc's file system fs.

package godoc

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/format"
	"go/parser"
	"go/token"
	pathpkg "path"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/buildutil"
	"golang.org/x/tools/godoc/vfs"
)

var linePrefix = []byte("//line ")

// This function replaces source lines starting with "//line " with a blank line.
// It does this irrespective of whether the line is truly a line comment or not;
// e.g., the line may be inside a string, or a /*-style comment; however that is
// rather unlikely (proper testing would require a full Go scan which we want to
// avoid for performance).
func replaceLinePrefixCommentsWithBlankLine(src []byte) {
	for {
		i := bytes.Index(src, linePrefix)
		if i < 0 {
			break // we're done
		}
		// 0 <= i && i+len(linePrefix) <= len(src)
		if i == 0 || src[i-1] == '\n' {
			// at beginning of line: blank out line
			for i < len(src) && src[i] != '\n' {
				src[i] = ' '
				i++
			}
		} else {
			// not at beginning of line: skip over prefix
			i += len(linePrefix)
		}
		// i <= len(src)
		src = src[i:]
	}
}

func findBuildTags(file *ast.File, allowedBuildTags []string) ([]string, bool) {
	var tags []string
	for _, c := range file.Comments {
		for _, l := range c.List {
			if !strings.Contains(l.Text, "+build") {
				continue
			}
			var tf buildutil.TagsFlag
			_ = tf.Set(l.Text)
			for _, t := range tf {
				for _, abt := range allowedBuildTags {
					if t == abt {
						tags = append(tags, t)
					}
				}
			}
		}
	}
	return tags, len(tags) > 0
}

// mapIdentifierToBuildTag maps the identifier (type/var/const/func/method) to
// the build tag. E.g. various method receivers can be scattered over different
// files with different build tags. The return value map[string]string contains
// as key the identifier name and the value are the build tags as comma
// separated list.
func (c *Corpus) mapIdentifierToBuildTag(files map[string]*ast.File, relpath, abspath string, ctxt *build.Context) (map[string]string, error) {
	allowedBuildTags := ctxt.BuildTags
	// key=build tag, value=list of file names
	tagToFiles := map[string][]string{}

	for fName, fAst := range files {
		if bts, ok := findBuildTags(fAst, allowedBuildTags); ok {
			for _, bt := range bts {
				f := tagToFiles[bt]

				f = append(f, filepath.Base(fName))
				tagToFiles[bt] = f
			}
		}
	}

	typesWithTags := map[string]string{}
	if len(tagToFiles) == 0 {
		return typesWithTags, nil
	}

	appendName := func(typeName, receiverType, tagName string) {
		var buf strings.Builder
		if receiverType != "" {
			buf.WriteString(receiverType)
			buf.WriteByte('.')
		}
		buf.WriteString(typeName)
		key := buf.String()

		tns := typesWithTags[key]
		if tns == "" {
			tns = tagName
		} else {
			tns = tns + ", " + tagName
		}
		typesWithTags[key] = tns
	}

	for tagName, fileNames := range tagToFiles {

		fset := token.NewFileSet()
		// Not possible to use go/doc.New because we're reading build tag files
		// only. Those files might reference to other types from other
		// non-build-tagged & not-parsed files. go/doc ignores those
		// functions/types. With parseFiles and the following code we can get
		// all identifiers from the files with a dedicated build tag.
		astMap, err := c.parseFiles(fset, relpath, abspath, fileNames)
		if err != nil {
			return nil, fmt.Errorf("%s in %q with files: %v", err.Error(), abspath, fileNames)
		}

		for _, fileAst := range astMap {
			for _, decl := range fileAst.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					receiverType := getReceiverType(fset, decl)
					appendName(decl.Name.String(), receiverType, tagName)

				case *ast.GenDecl:
					for _, spec := range decl.Specs {
						switch spec := spec.(type) {
						case *ast.TypeSpec:
							appendName(spec.Name.String(), "", tagName)

						case *ast.ValueSpec:
							for _, id := range spec.Names {
								appendName(id.Name, "", tagName)
							}
						}
					}
				}
			}
		}
	}

	return typesWithTags, nil
}

func getReceiverType(fset *token.FileSet, decl *ast.FuncDecl) string {
	if decl.Recv == nil {
		return ""
	}

	var buf strings.Builder
	if err := format.Node(&buf, fset, decl.Recv.List[0].Type); err != nil {
		return ""
	}

	return buf.String()
}

func (c *Corpus) parseFile(fset *token.FileSet, filename string, mode parser.Mode) (*ast.File, error) {
	src, err := vfs.ReadFile(c.fs, filename)
	if err != nil {
		return nil, err
	}

	// Temporary ad-hoc fix for issue 5247.
	// TODO(gri) Remove this in favor of a better fix, eventually (see issue 7702).
	replaceLinePrefixCommentsWithBlankLine(src)

	return parser.ParseFile(fset, filename, src, mode)
}

func (c *Corpus) parseFiles(fset *token.FileSet, relpath string, abspath string, localnames []string) (map[string]*ast.File, error) {
	files := make(map[string]*ast.File)
	for _, f := range localnames {
		absname := pathpkg.Join(abspath, f)
		file, err := c.parseFile(fset, absname, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files[pathpkg.Join(relpath, f)] = file
	}

	return files, nil
}
