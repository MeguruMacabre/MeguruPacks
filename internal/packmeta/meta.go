package packmeta

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	FileName       = ".megurupacks.json"
	defaultVersion = "0.1.0"
)

type Meta struct {
	PackID      string `json:"pack_id"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
}

func Read(packDir string) (Meta, bool, error) {
	path := filepath.Join(packDir, FileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Meta{}, false, nil
		}
		return Meta{}, false, err
	}

	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Meta{}, false, err
	}

	return normalize(meta), true, nil
}

func Ensure(packDir, folderName string) (Meta, error) {
	meta, exists, err := Read(packDir)
	if err != nil {
		return Meta{}, err
	}

	meta, changed := ensureDefaults(meta, folderName, !exists)
	meta = normalize(meta)

	if changed {
		if err := Save(packDir, meta); err != nil {
			return Meta{}, err
		}
	}

	return meta, nil
}

func Save(packDir string, meta Meta) error {
	meta = normalize(meta)

	path := filepath.Join(packDir, FileName)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

func VisibleName(meta Meta, folderName string) string {
	if strings.TrimSpace(meta.DisplayName) != "" {
		return strings.TrimSpace(meta.DisplayName)
	}
	return folderName
}

func VisibleVersion(meta Meta) string {
	if strings.TrimSpace(meta.Version) != "" {
		return strings.TrimSpace(meta.Version)
	}
	return defaultVersion
}

func PreviewVersion(version, mode string, delta int) (string, error) {
	major, minor, patch, err := parseVersion(version)
	if err != nil {
		return "", err
	}

	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "patch":
		patch += delta
		if patch < 0 {
			patch = 0
		}
	case "minor":
		minor += delta
		if minor < 0 {
			minor = 0
		}
		patch = 0
	case "major":
		major += delta
		if major < 0 {
			major = 0
		}
		minor = 0
		patch = 0
	default:
		return "", fmt.Errorf("unknown version mode: %s", mode)
	}

	return fmt.Sprintf("%d.%d.%d", major, minor, patch), nil
}

func ensureDefaults(meta Meta, folderName string, forceCreate bool) (Meta, bool) {
	changed := false

	if forceCreate {
		meta = Meta{}
		changed = true
	}

	if strings.TrimSpace(meta.PackID) == "" {
		meta.PackID = newPackID()
		changed = true
	}

	if strings.TrimSpace(meta.DisplayName) == "" {
		meta.DisplayName = folderName
		changed = true
	}

	if strings.TrimSpace(meta.Version) == "" {
		meta.Version = defaultVersion
		changed = true
	}

	return meta, changed
}

func normalize(meta Meta) Meta {
	meta.PackID = strings.TrimSpace(meta.PackID)
	meta.DisplayName = strings.TrimSpace(meta.DisplayName)
	meta.Version = strings.TrimSpace(meta.Version)

	if meta.Version == "" {
		meta.Version = defaultVersion
	}

	return meta
}

func parseVersion(v string) (int, int, int, error) {
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid version format: %s", v)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid major version: %s", v)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid minor version: %s", v)
	}

	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid patch version: %s", v)
	}

	if major < 0 || minor < 0 || patch < 0 {
		return 0, 0, 0, fmt.Errorf("version parts must be >= 0: %s", v)
	}

	return major, minor, patch, nil
}

func newPackID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return "pack_" + hex.EncodeToString(buf)
}
