package removelocal

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MeguruMacabre/MeguruPacks/internal/clientstate"
	"github.com/MeguruMacabre/MeguruPacks/internal/serverpacks"
)

type Result struct {
	PackName   string
	PackID     string
	TargetDir  string
	DeletedAt  time.Time
	WasPresent bool
}

func Pack(installRoot string, pack serverpacks.Pack) (Result, error) {
	targetDir := filepath.Join(installRoot, safeDirName(pack.PackName))

	state, exists, err := clientstate.Read(targetDir)
	if err != nil {
		return Result{}, err
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return Result{}, err
	}

	packName := pack.PackName
	packID := pack.PackID

	if exists {
		if strings.TrimSpace(state.PackName) != "" {
			packName = state.PackName
		}
		if strings.TrimSpace(state.PackID) != "" {
			packID = state.PackID
		}
	}

	return Result{
		PackName:   packName,
		PackID:     packID,
		TargetDir:  targetDir,
		DeletedAt:  time.Now().UTC(),
		WasPresent: exists,
	}, nil
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
