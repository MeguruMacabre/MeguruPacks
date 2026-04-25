package clientmanifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/MeguruMacabre/MeguruPacks/internal/manifest"
)

const FileName = ".megurupacks-manifest.json"

func Read(packDir string) (manifest.Manifest, bool, error) {
	path := filepath.Join(packDir, FileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return manifest.Manifest{}, false, nil
		}
		return manifest.Manifest{}, false, err
	}

	var mf manifest.Manifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return manifest.Manifest{}, false, err
	}

	return normalize(mf), true, nil
}

func Save(packDir string, mf manifest.Manifest) error {
	mf = normalize(mf)

	path := filepath.Join(packDir, FileName)
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

func ManagedPaths(mf manifest.Manifest) map[string]manifest.FileEntry {
	result := make(map[string]manifest.FileEntry, len(mf.Files))
	for _, f := range mf.Files {
		p := strings.TrimSpace(filepath.Clean(filepath.FromSlash(f.Path)))
		if p == "" || p == "." {
			continue
		}
		result[p] = f
	}
	return result
}

func normalize(mf manifest.Manifest) manifest.Manifest {
	mf.PackName = strings.TrimSpace(mf.PackName)
	mf.PackID = strings.TrimSpace(mf.PackID)
	mf.Version = strings.TrimSpace(mf.Version)
	return mf
}
