package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIsProductPackage(t *testing.T) {
	moduleDirectory := t.TempDir()
	module := &listedModule{
		Path: "example.com/mediaarchive",
		Dir:  moduleDirectory,
		Main: true,
	}

	testCases := []struct {
		name     string
		pkg      listedPackage
		expected bool
	}{
		{
			name: "product package",
			pkg: listedPackage{
				Dir:    filepath.Join(moduleDirectory, "internal", "identity"),
				Module: module,
			},
			expected: true,
		},
		{
			name: "architecture tool",
			pkg: listedPackage{
				Dir:    filepath.Join(moduleDirectory, "tools", "archdoc"),
				Module: module,
			},
			expected: false,
		},
		{
			name: "dependency package",
			pkg: listedPackage{
				Dir: filepath.Join(moduleDirectory, "dependency"),
				Module: &listedModule{
					Path: "example.com/dependency",
					Dir:  filepath.Join(moduleDirectory, "dependency"),
				},
			},
			expected: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if actual := isProductPackage(testCase.pkg); actual != testCase.expected {
				t.Fatalf("expected %t, got %t", testCase.expected, actual)
			}
		})
	}
}

func TestGroupForPackageClassifiesClientAdapters(t *testing.T) {
	importPaths := []string{
		"example.com/mediaarchive/internal/cli",
		"example.com/mediaarchive/internal/client",
	}

	for _, importPath := range importPaths {
		group := groupForPackage("example.com/mediaarchive", importPath)
		if group != "client" {
			t.Errorf("expected client group for %q, got %q", importPath, group)
		}
	}
}

func TestGenerateDocumentSortsPackagesAndIncludesInternalImports(t *testing.T) {
	moduleDirectory := t.TempDir()
	module := &listedModule{
		Path: "example.com/mediaarchive",
		Dir:  moduleDirectory,
		Main: true,
	}
	packages := []listedPackage{
		{
			ImportPath: "example.com/mediaarchive/internal/identity",
			Name:       "identity",
			Doc:        "Package identity defines users.",
			Dir:        filepath.Join(moduleDirectory, "internal", "identity"),
			Module:     module,
		},
		{
			ImportPath: "example.com/mediaarchive/cmd/server",
			Name:       "main",
			Dir:        filepath.Join(moduleDirectory, "cmd", "server"),
			Imports: []string{
				"example.com/mediaarchive/internal/identity",
				"net/http",
			},
			Module: module,
		},
	}

	firstDocument, err := generateDocument(packages)
	if err != nil {
		t.Fatalf("generate first document: %v", err)
	}
	secondDocument, err := generateDocument(packages)
	if err != nil {
		t.Fatalf("generate second document: %v", err)
	}
	if string(firstDocument) != string(secondDocument) {
		t.Fatal("expected deterministic document output")
	}

	document := string(firstDocument)
	if !strings.Contains(document, "package0 --> package1") {
		t.Fatal("expected internal package import")
	}
	if strings.Contains(document, "net/http") {
		t.Fatal("expected standard-library imports to be omitted")
	}
	if !strings.Contains(document, "Package identity defines users.") {
		t.Fatal("expected package documentation")
	}
	if !strings.Contains(document, "[Database structure](database.md)") {
		t.Fatal("expected database documentation link")
	}
}
