package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	PackName      string
	PackID        string
	Version       string
	TargetDir     string
	ManifestKey   string
	Downloaded    int
	Skipped       int
	InstalledAt   time.Time
	InstalledSize int64
}

func InstallPack(
	ctx context.Context,
	store *storage.Client,
	pack serverpacks.Pack,
	packsDir string,
	progressCh chan<- ProgressUpdate,
) (Result, error) {
	manifestBytes, err := store.ReadFullKeyBytes(ctx, pack.ManifestKey)
	if err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Reading manifest",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	var mf manifest.Manifest
	if err := json.Unmarshal(manifestBytes, &mf); err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Parsing manifest",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	targetDir := filepath.Join(packsDir, safeDirName(pack.PackName))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Preparing install dir",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	downloaded := 0
	skipped := 0
	var doneBytes int64

	for i, file := range mf.Files {
		targetPath := filepath.Join(targetDir, filepath.FromSlash(file.Path))

		localMatches, err := localFileMatches(targetPath, file.SHA256)
		if err != nil {
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Checking local files",
				CurrentFile: file.Path,
				FilesDone:   i,
				FilesTotal:  len(mf.Files),
				BytesDone:   doneBytes,
				BytesTotal:  mf.SizeBytes,
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
				FilesTotal:  len(mf.Files),
				BytesDone:   doneBytes,
				BytesTotal:  mf.SizeBytes,
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
				FilesTotal:  len(mf.Files),
				BytesDone:   doneBytes,
				BytesTotal:  mf.SizeBytes,
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
				FilesTotal:  len(mf.Files),
				BytesDone:   doneBytes,
				BytesTotal:  mf.SizeBytes,
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
				FilesTotal:  len(mf.Files),
				BytesDone:   doneBytes,
				BytesTotal:  mf.SizeBytes,
				Done:        true,
				Err:         err,
			})
			return Result{}, err
		}

		_ = os.Chtimes(targetPath, file.ModifiedAt, file.ModifiedAt)

		downloaded++
		doneBytes += file.SizeBytes

		emitProgress(progressCh, ProgressUpdate{
			Stage:       "Installing files",
			CurrentFile: file.Path,
			FilesDone:   i + 1,
			FilesTotal:  len(mf.Files),
			BytesDone:   doneBytes,
			BytesTotal:  mf.SizeBytes,
		})
	}

	state := clientstate.State{
		PackID:      nonEmpty(mf.PackID, pack.PackID),
		PackName:    nonEmpty(mf.PackName, pack.PackName),
		Version:     nonEmpty(mf.Version, pack.Version),
		ManifestKey: pack.ManifestKey,
		InstalledAt: time.Now().UTC(),
	}

	if err := clientstate.Save(targetDir, state); err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Saving client state",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	if err := clientmanifest.Save(targetDir, mf); err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Saving local manifest",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	result := Result{
		PackName:      state.PackName,
		PackID:        state.PackID,
		Version:       state.Version,
		TargetDir:     targetDir,
		ManifestKey:   pack.ManifestKey,
		Downloaded:    downloaded,
		Skipped:       skipped,
		InstalledAt:   state.InstalledAt,
		InstalledSize: mf.SizeBytes,
	}

	emitProgress(progressCh, ProgressUpdate{
		Stage:      "Done",
		FilesDone:  len(mf.Files),
		FilesTotal: len(mf.Files),
		BytesDone:  mf.SizeBytes,
		BytesTotal: mf.SizeBytes,
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

func emitProgress(ch chan<- ProgressUpdate, update ProgressUpdate) {
	if ch == nil {
		return
	}

	select {
	case ch <- update:
	default:
	}
}
