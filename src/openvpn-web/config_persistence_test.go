package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/viper"
)

func TestBootstrapConfigPersistenceReplacesRatherThanTruncates(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	backup := filepath.Join(dir, "old-config.json")
	original := []byte(`{"system":{"base":{"admin_username":"admin"}}}`)
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, backup); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigFile(target)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENVPN_UI_ADMIN_PASSWORD", "ConfiguredPassword!123")
	if err := initializeAdministrator(v, dir, true); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(backup)
	if err != nil || string(old) != string(original) {
		t.Fatalf("configuration was truncated/rewritten in place: %s %v", old, err)
	}
	reloaded := viper.New()
	reloaded.SetConfigFile(target)
	if err := reloaded.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if reloaded.GetString("system.base.admin_password") != v.GetString("system.base.admin_password") {
		t.Fatal("bootstrap credential not persisted")
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("configuration permissions: %v %v", info, err)
	}
}

func TestConfigPersistenceWriteFailurePreservesOriginal(t *testing.T) {
	if os.Getenv("OPENVPN_CONFIG_WRITE_FAULT_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestConfigPersistenceWriteFailurePreservesOriginal$")
		cmd.Env = append(os.Environ(), "OPENVPN_CONFIG_WRITE_FAULT_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("write fault subprocess: %v %s", err, output)
		}
		return
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	original := []byte(`{"credential":"original"}`)
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigFile(target)
	v.Set("credential", strings.Repeat("x", 8192))
	// Real kernel write failure, isolated to this subprocess: the destination must
	// remain byte-identical when the temporary file write stops part way through.
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 64, Max: 64}); err != nil {
		t.Fatal(err)
	}
	if err := persistConfig(v); err == nil {
		t.Fatal("short write accepted")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(original) {
		t.Fatalf("partial write damaged config: %s %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary config leaked: %v %v", entries, err)
	}
}

func TestConfigPersistenceFailuresAndExclusiveCreation(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	v := viper.New()
	v.SetConfigFile(target)
	v.Set("credential", "original")
	if err := persistNewConfig(v, target); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	v.Set("credential", "replacement")
	if err := persistNewConfig(v, target); !os.IsExist(err) {
		t.Fatalf("exclusive creation replaced existing config: %v", err)
	}
	v.Set("invalid", make(chan int))
	if err := persistConfig(v); err == nil {
		t.Fatal("unserializable configuration accepted")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(original) {
		t.Fatal("failed persistence changed original")
	}
	v = viper.New()
	v.SetConfigFile(dir)
	v.Set("credential", "replacement")
	if err := persistConfig(v); err == nil {
		t.Fatal("rename failure accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed persistence leaked temporary files: %v %v", entries, err)
	}
}

func TestBootstrapPersistenceFailureDoesNotInstallLiveCredential(t *testing.T) {
	dir := t.TempDir()
	v := viper.New()
	v.SetConfigFile(filepath.Join(dir, "missing", "config.json"))
	t.Setenv("OPENVPN_UI_ADMIN_PASSWORD", "ConfiguredPassword!123")
	if err := initializeAdministrator(v, dir, true); err == nil {
		t.Fatal("bootstrap persistence failure accepted")
	}
	if v.GetString("system.base.admin_password") != "" || v.GetString("system.base.admin_session_version") != "" || v.GetBool("system.base.admin_first_login") {
		t.Fatal("failed bootstrap installed live credential")
	}
}
