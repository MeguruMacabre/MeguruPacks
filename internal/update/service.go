package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MeguruMacabre/MeguruPacks/internal/clientmanifest"
	"github.com/MeguruMacabre/MeguruPacks/internal/clientstate"
	"github.com/MeguruMacabre/MeguruPacks/internal/manifest"
	"github.com/MeguruMacabre/MeguruPacks/internal/serverpacks"
	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
)

type ProgressUpdate struct {
	Stage       string
	CurrentFile string
	FilesDone   int
	FilesTotal  int
	BytesDone   int64
	BytesTotal  int64
	Done        bool
	Err         error
}

type Result struct {
	PackName     string
	PackID       string
	Version      string
	TargetDir    string
	ManifestKey  string
	Downloaded   int
	Skipped      int
	Deleted      int
	UpdatedAt    time.Time
	UpdatedSize  int64
	LocalVersion string
}

func Pack(
	ctx context.Context,
	store *storage.Client,
	pack serverpacks.Pack,
	packsDir string,
	progressCh chan<- ProgressUpdate,
) (Result, error) {
	targetDir := filepath.Join(packsDir, safeDirName(pack.PackName))

	state, exists, err := clientstate.Read(targetDir)
	if err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Reading local state",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}
	if !exists {
		err := fmt.Errorf("pack is not installed: %s", targetDir)
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Reading local state",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}
	if strings.TrimSpace(state.PackID) != "" && strings.TrimSpace(pack.PackID) != "" && state.PackID != pack.PackID {
		err := fmt.Errorf("installed pack id mismatch: local=%s remote=%s", state.PackID, pack.PackID)
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Validating local install",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	oldManifest, _, err := clientmanifest.Read(targetDir)
	if err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Reading local manifest",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	manifestBytes, err := store.ReadFullKeyBytes(ctx, pack.ManifestKey)
	if err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Reading remote manifest",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	var newManifest manifest.Manifest
	if err := json.Unmarshal(manifestBytes, &newManifest); err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Parsing remote manifest",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	oldPaths := clientmanifest.ManagedPaths(oldManifest)
	newPaths := clientmanifest.ManagedPaths(newManifest)

	deleted := 0

	toDelete := make([]string, 0)
	for rel := range oldPaths {
		if _, ok := newPaths[rel]; ok {
			continue
		}
		toDelete = append(toDelete, rel)
	}
	sort.Strings(toDelete)

	for i, rel := range toDelete {
		fullPath := filepath.Join(targetDir, rel)
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Deleting removed files",
				CurrentFile: rel,
				FilesDone:   i,
				FilesTotal:  len(toDelete),
				Done:        true,
				Err:         err,
			})
			return Result{}, err
		}

		_ = removeEmptyParents(targetDir, filepath.Dir(fullPath))
		deleted++

		emitProgress(progressCh, ProgressUpdate{
			Stage:       "Deleting removed files",
			CurrentFile: rel,
			FilesDone:   i + 1,
			FilesTotal:  len(toDelete),
		})
	}

	downloaded := 0
	skipped := 0
	var doneBytes int64

	for i, file := range newManifest.Files {
		targetPath := filepath.Join(targetDir, filepath.FromSlash(file.Path))

		localMatches, err := localFileMatches(targetPath, file.SHA256)
		if err != nil {
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Checking local files",
				CurrentFile: file.Path,
				FilesDone:   i,
				FilesTotal:  len(newManifest.Files),
				BytesDone:   doneBytes,
				BytesTotal:  newManifest.SizeBytes,
				Done:        true,
				Err:         err,
			})
			return Result{}, err
		}

		if localMatches {
			skipped++
			doneBytes += file.SizeBytes
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Checking local files",
				CurrentFile: file.Path,
				FilesDone:   i + 1,
				FilesTotal:  len(newManifest.Files),
				BytesDone:   doneBytes,
				BytesTotal:  newManifest.SizeBytes,
			})
			continue
		}

		objectFullKey := store.FullKey(objectKeyForHash(file.SHA256))
		data, err := store.ReadFullKeyBytes(ctx, objectFullKey)
		if err != nil {
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Downloading objects",
				CurrentFile: file.Path,
				FilesDone:   i,
				FilesTotal:  len(newManifest.Files),
				BytesDone:   doneBytes,
				BytesTotal:  newManifest.SizeBytes,
				Done:        true,
				Err:         err,
			})
			return Result{}, err
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Preparing file path",
				CurrentFile: file.Path,
				FilesDone:   i,
				FilesTotal:  len(newManifest.Files),
				BytesDone:   doneBytes,
				BytesTotal:  newManifest.SizeBytes,
				Done:        true,
				Err:         err,
			})
			return Result{}, err
		}

		if err := os.WriteFile(targetPath, data, 0o644); err != nil {
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Writing files",
				CurrentFile: file.Path,
				FilesDone:   i,
				FilesTotal:  len(newManifest.Files),
				BytesDone:   doneBytes,
				BytesTotal:  newManifest.SizeBytes,
				Done:        true,
				Err:         err,
			})
			return Result{}, err
		}

		_ = os.Chtimes(targetPath, file.ModifiedAt, file.ModifiedAt)

		downloaded++
		doneBytes += file.SizeBytes

		emitProgress(progressCh, ProgressUpdate{
			Stage:       "Updating files",
			CurrentFile: file.Path,
			FilesDone:   i + 1,
			FilesTotal:  len(newManifest.Files),
			BytesDone:   doneBytes,
			BytesTotal:  newManifest.SizeBytes,
		})
	}

	now := time.Now().UTC()
	newState := clientstate.State{
		PackID:      nonEmpty(newManifest.PackID, pack.PackID),
		PackName:    nonEmpty(newManifest.PackName, pack.PackName),
		Version:     nonEmpty(newManifest.Version, pack.Version),
		ManifestKey: pack.ManifestKey,
		InstalledAt: now,
	}

	if err := clientstate.Save(targetDir, newState); err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Saving client state",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	if err := clientmanifest.Save(targetDir, newManifest); err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Saving local manifest",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	result := Result{
		PackName:     newState.PackName,
		PackID:       newState.PackID,
		Version:      newState.Version,
		TargetDir:    targetDir,
		ManifestKey:  pack.ManifestKey,
		Downloaded:   downloaded,
		Skipped:      skipped,
		Deleted:      deleted,
		UpdatedAt:    now,
		UpdatedSize:  newManifest.SizeBytes,
		LocalVersion: state.Version,
	}

	emitProgress(progressCh, ProgressUpdate{
		Stage:      "Done",
		FilesDone:  len(newManifest.Files),
		FilesTotal: len(newManifest.Files),
		BytesDone:  newManifest.SizeBytes,
		BytesTotal: newManifest.SizeBytes,
		Done:       true,
	})

	return result, nil
}

func localFileMatches(path string, expectedSHA256 string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}

	sum, err := fileSHA256(path)
	if err != nil {
		return false, err
	}

	return strings.EqualFold(sum, expectedSHA256), nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func objectKeyForHash(hash string) string {
	if len(hash) < 4 {
		return "objects/" + hash
	}
	return fmt.Sprintf("objects/%s/%s/%s", hash[:2], hash[2:4], hash)
}

func safeDirName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "" {
		return "pack"
	}
	return name
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func removeEmptyParents(root string, dir string) error {
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)

	for {
		if dir == root || dir == "." || dir == string(filepath.Separator) {
			return nil
		}

		err := os.Remove(dir)
		if err != nil {
			if os.IsNotExist(err) {
				dir = filepath.Dir(dir)
				continue
			}
			return nil
		}

		dir = filepath.Dir(dir)
	}
}

func emitProgress(ch chan<- ProgressUpdate, update ProgressUpdate) {
	if ch == nil {
		return
	}

	select {
	case ch <- update:
	default:
	}
}
