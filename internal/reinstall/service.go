package reinstall

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/MeguruMacabre/MeguruPacks/internal/install"
	"github.com/MeguruMacabre/MeguruPacks/internal/serverpacks"
	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
)

type ProgressUpdate = install.ProgressUpdate
type Result = install.Result

func Pack(
	ctx context.Context,
	store *storage.Client,
	pack serverpacks.Pack,
	installRoot string,
	progressCh chan<- ProgressUpdate,
) (Result, error) {
	targetDir := filepath.Join(installRoot, safeDirName(pack.PackName))

	if err := os.RemoveAll(targetDir); err != nil {
		emit(progressCh, ProgressUpdate{
			Stage: "Removing local pack",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	emit(progressCh, ProgressUpdate{
		Stage: "Removed local pack",
	})

	return install.InstallPack(ctx, store, pack, installRoot, progressCh)
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

func emit(ch chan<- ProgressUpdate, update ProgressUpdate) {
	if ch == nil {
		return
	}
	select {
	case ch <- update:
	default:
	}
}
