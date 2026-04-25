package clientstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const FileName = ".megurupacks-client.json"

type State struct {
	PackID      string    `json:"pack_id"`
	PackName    string    `json:"pack_name"`
	Version     string    `json:"version"`
	ManifestKey string    `json:"manifest_key"`
	InstalledAt time.Time `json:"installed_at"`
}

func Read(packDir string) (State, bool, error) {
	path := filepath.Join(packDir, FileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, false, nil
		}
		return State{}, false, err
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, false, err
	}

	return normalize(state), true, nil
}

func Save(packDir string, state State) error {
	state = normalize(state)

	path := filepath.Join(packDir, FileName)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

func normalize(state State) State {
	state.PackID = strings.TrimSpace(state.PackID)
	state.PackName = strings.TrimSpace(state.PackName)
	state.Version = strings.TrimSpace(state.Version)
	state.ManifestKey = strings.TrimSpace(state.ManifestKey)
	return state
}
