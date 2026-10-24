package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// persistConfig serializes before touching disk and atomically replaces the
// private JSON file. Unlike Viper.WriteConfig it never truncates the live file;
// Failures before publication leave it intact. Parent-directory sync errors occur
// AFTER publication: new content is visible but crash durability is uncertain.
// Callers serialize configuration mutations with configRequestGuard.
func persistConfig(v *viper.Viper) error {
	target := v.ConfigFileUsed()
	if target == "" {
		return errors.New("configuration file is not selected")
	}
	content, err := json.MarshalIndent(v.AllSettings(), "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivateConfig(target, append(content, '\n'))
}

// Initial creation must also publish only a fully written file, but must not
// replace a configuration that appeared since the first-run existence check.
func persistNewConfig(v *viper.Viper, target string) error {
	content, err := json.MarshalIndent(v.AllSettings(), "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(append(content, '\n')); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Link(file.Name(), target); err != nil {
		return err
	}
	return syncPublishedConfigParent(target, (*os.File).Sync)
}

// A successful rename/link is not crash-durable until its parent is synced.
// Never imply that a sync failure undid publication.
func syncPublishedConfigParent(target string, syncDir func(*os.File) error) error {
	dir, err := os.Open(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("configuration published; parent directory open failed (durability uncertain): %w", err)
	}
	return syncPublishedConfigDirectory(dir, syncDir)
}

func syncPublishedConfigDirectory(dir *os.File, syncDir func(*os.File) error) error {
	syncErr := syncDir(dir)
	closeErr := dir.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("configuration published; parent directory sync/close failed (durability uncertain): %w", err)
	}
	return nil
}
