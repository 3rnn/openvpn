package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// persistConfig serializes before touching disk and atomically replaces the
// private JSON file. Unlike Viper.WriteConfig it never truncates the live file;
// serialization, temporary write, sync, close and rename errors leave it intact.
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
	return os.Link(file.Name(), target)
}
