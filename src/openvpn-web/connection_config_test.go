package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestConnectionConfigIsolation(t *testing.T) {
	old := ovData
	ovData = t.TempDir()
	defer func() { ovData = old }()
	var wg sync.WaitGroup
	for _, name := range []string{"alice", "bob"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := writeClientConnectionConfig(name, "cert", "10.8.0.9", "push \"route 10.0.0.0 255.0.0.0\""); err != nil {
				t.Error(err)
			}
		}(name)
	}
	wg.Wait()
	entries, e := os.ReadDir(filepath.Join(ovData, "connections"))
	if e != nil || len(entries) != 2 {
		t.Fatalf("isolation: %v %v", entries, e)
	}
	for _, entry := range entries {
		info, _ := entry.Info()
		if info.Mode().Perm() != 0600 {
			t.Fatal("unsafe permissions")
		}
		b, _ := os.ReadFile(filepath.Join(ovData, "connections", entry.Name()))
		if !strings.Contains(string(b), "ifconfig-push 10.8.0.9") {
			t.Fatal(string(b))
		}
	}
	if err := writeClientConnectionConfig("../bad", "cert", "", ""); err == nil {
		t.Fatal("traversal accepted")
	}
	if err := writeClientConnectionConfig("alice", "cert", "1.2.3.4\nup evil", ""); err == nil {
		t.Fatal("IP injection accepted")
	}
	if err := writeClientConnectionConfig("alice", "cert", "", "up /bin/evil"); err == nil {
		t.Fatal("command directive accepted")
	}
}
