package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicConfigDirectorySyncFailureReportsPublishedState(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	err := atomicPrivateConfigWithSync(target, []byte("new"), func(dir *os.File) error {
		called = true
		info, e := dir.Stat()
		if e != nil || !info.IsDir() {
			t.Fatal("sync target is not directory")
		}
		got, e := os.ReadFile(target)
		if e != nil || string(got) != "new" {
			t.Fatal("directory synced before rename")
		}
		return errors.New("injected fsync failure")
	})
	if !called || err == nil || !strings.Contains(err.Error(), "published") {
		t.Fatalf("missing post-publication durability error: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatal("post-publication error falsely claimed unchanged file")
	}
}
