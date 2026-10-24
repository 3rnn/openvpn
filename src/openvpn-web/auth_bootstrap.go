package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
)

// initializeAdministrator never replaces an existing credential. Environment
// input is bootstrap-only. A damaged existing configuration requires explicit,
// offline operator recovery, never a fallback to a public default.
func initializeAdministrator(v *viper.Viper, dataDir string, firstRun bool) error {
	if v.GetString("system.base.admin_password") != "" {
		return nil
	}
	if !firstRun {
		return errors.New("existing configuration has no administrator credential; restore a known-good configuration or follow offline recovery")
	}
	password := os.Getenv("OPENVPN_UI_ADMIN_PASSWORD")
	generated := password == ""
	if generated {
		password = "Aa1!" + genRandomString(32)
	}
	if !isValidPassword(password) || len(password) > 72 {
		return errors.New("OPENVPN_UI_ADMIN_PASSWORD must be 12-72 bytes and contain uppercase, lowercase, digit and punctuation")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return errors.New("administrator password initialization failed")
	}
	if generated {
		path := filepath.Join(dataDir, "bootstrap-admin-password")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("cannot create private administrator bootstrap file: %w", err)
		}
		if _, err = file.WriteString(password + "\n"); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return errors.New("cannot write private administrator bootstrap file")
		}
	}
	v.Set("system.base.admin_password", string(hash))
	v.Set("system.base.admin_first_login", true)
	v.Set("system.base.admin_session_version", secureToken())
	if err := v.WriteConfig(); err != nil {
		return errors.New("cannot persist administrator bootstrap configuration")
	}
	return nil
}
