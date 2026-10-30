package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCommittedAnchorsMatchTheSECFixtures(t *testing.T) {
	files, err := build(filepath.Join("..", "..", "..", "testdata", "sec"),
		filepath.Join("..", "..", "..", "tools", "price-fixtures", "config.json"))
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if len(files) == 0 {
		t.Fatal("build() wrote no anchors file")
	}
	for path, want := range files {
		got, err := os.ReadFile(path) //nolint:gosec // a path from the committed config
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s differs from the anchors that the SEC fixtures give (%v); run make generate-price-fixtures", path, err)
		}
	}
}
