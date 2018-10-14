// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package godoc

import (
	"go/build"
	"go/token"
	"testing"

	"golang.org/x/tools/godoc/vfs"
	"golang.org/x/tools/godoc/vfs/mapfs"
)

func TestCorpus_ExtractBuildTags(t *testing.T) {
	// cleanup := setupGoroot(t)
	// defer cleanup()

	mfs := mapfs.New(map[string]string{
		"src/bar.go": `// Package bar is an example.
package bar

var WunderBar = "Cocktails"

type TrinkBar interface {
	Cheers()
}

`,
		"src/xtag1.go": `
// +build xtag1

package bar

// First function is first.
func First() {
}

// unexported function is third.
func unexported() {
}

type A struct {}

func (a A) String() string { return "" }
`,
		"src/xtag2.go": `
// +build xtag2 xtag3

package bar

func NewCheersWithBeer() TrinkBar { return nil }

const TestConst = true
`,
	})
	fs := make(vfs.NameSpace)
	fs.Bind("/", mfs, "/", vfs.BindReplace)
	c := NewCorpus(fs)

	build.Default.BuildTags = []string{"xtag1", "xtag2", "xtag3"}

	fset := token.NewFileSet()
	fast, err := c.parseFiles(fset, "", "/src", []string{"bar.go", "xtag1.go", "xtag2.go"})
	if err != nil {
		t.Fatalf("%s", err)
	}

	tagMap, err := c.mapIdentifierToBuildTag(fast, "", "/src", &build.Default)
	if err != nil {
		t.Fatalf("%s", err)
	}

	haveWant := func(key, want string) {
		if tagMap[key] != want {
			t.Errorf("Have: %q Want: %q", tagMap[key], want)
		}
	}
	haveWant("First", "xtag1")
	haveWant("unexported", "xtag1")
	haveWant("NewCheersWithBeer", "xtag2, xtag3")
	haveWant("A.String", "xtag1")
	haveWant("TestConst", "xtag2, xtag3")
}
