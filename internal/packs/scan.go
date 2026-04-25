package packs

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MeguruMacabre/MeguruPacks/internal/packmeta"
)

func ScanRoot(root string) ([]Pack, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var result []Pack

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if shouldIgnoreTopDir(name) {
			continue
		}

		packPath := filepath.Join(root, name)

		pack, err := scanPack(packPath, name)
		if err != nil {
			return nil, err
		}

		result = append(result, pack)
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})

	return result, nil
}

func scanPack(root, folderName string) (Pack, error) {
	meta, exists, err := packmeta.Read(root)
	if err != nil {
		return Pack{}, err
	}

	displayName := folderName
	version := "0.1.0"
	packID := ""

	if exists {
		displayName = packmeta.VisibleName(meta, folderName)
		version = packmeta.VisibleVersion(meta)
		packID = meta.PackID
	}

	pack := Pack{
		Name:       displayName,
		FolderName: folderName,
		Path:       root,
		PackID:     packID,
		Version:    version,
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		if path == root {
			return nil
		}

		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if shouldIgnoreNestedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldIgnoreFile(d.Name()) {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}

		rel = filepath.ToSlash(rel)

		pack.Files = append(pack.Files, FileEntry{
			RelativePath: rel,
			SizeBytes:    info.Size(),
			ModifiedAt:   info.ModTime(),
		})

		pack.FileCount++
		pack.SizeBytes += info.Size()

		if pack.LastModified.IsZero() || info.ModTime().After(pack.LastModified) {
			pack.LastModified = info.ModTime()
		}

		return nil
	})
	if err != nil {
		return Pack{}, err
	}

	sort.Slice(pack.Files, func(i, j int) bool {
		return pack.Files[i].RelativePath < pack.Files[j].RelativePath
	})

	return pack, nil
}

func shouldIgnoreTopDir(name string) bool {
	if name == "__MACOSX" {
		return true
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	return false
}

func shouldIgnoreNestedDir(name string) bool {
	return name == "__MACOSX"
}

func shouldIgnoreFile(name string) bool {
	switch strings.ToLower(name) {
	case ".ds_store", "thumbs.db", "desktop.ini", packmeta.FileName:
		return true
	default:
		return false
	}
}
