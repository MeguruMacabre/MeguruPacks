package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/MeguruMacabre/MeguruPacks/internal/packs"
)

func Build(pack packs.Pack) (Manifest, error) {
	result := Manifest{
		PackName:    pack.Name,
		PackID:      pack.PackID,
		Version:     pack.Version,
		GeneratedAt: time.Now(),
		FileCount:   pack.FileCount,
		SizeBytes:   pack.SizeBytes,
		Files:       make([]FileEntry, 0, len(pack.Files)),
	}

	for _, file := range pack.Files {
		fullPath := filepath.Join(pack.Path, filepath.FromSlash(file.RelativePath))

		hash, err := fileSHA256(fullPath)
		if err != nil {
			return Manifest{}, err
		}

		result.Files = append(result.Files, FileEntry{
			Path:       file.RelativePath,
			SizeBytes:  file.SizeBytes,
			SHA256:     hash,
			ModifiedAt: file.ModifiedAt,
		})
	}

	sort.Slice(result.Files, func(i, j int) bool {
		return result.Files[i].Path < result.Files[j].Path
	})

	return result, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()

	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
