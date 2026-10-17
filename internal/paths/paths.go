// Package paths says where rflib keeps its files when it isn't told: the
// database, and the user's own radio models and export profiles.
package paths

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// ConfigDir is where rflib's own configuration goes: $XDG_CONFIG_HOME/rflib
// when that's set, on any system, or else the system's place for it
// (~/.config on Linux, ~/Library/Application Support on macOS, %AppData% on
// Windows).
func ConfigDir() (string, error) {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "rflib"), nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "rflib"), nil
}

// DataDir is where rflib's database goes: $XDG_DATA_HOME/rflib when that's
// set, on any system, or else %LOCALAPPDATA%\rflib on Windows and
// ~/.local/share/rflib elsewhere.
func DataDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "rflib"), nil
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "rflib"), nil
		}
		return "", errors.New("%LOCALAPPDATA% isn't set")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "rflib"), nil
}
