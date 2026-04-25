package autoupdate

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MeguruMacabre/MeguruPacks/internal/clientstate"
	"github.com/MeguruMacabre/MeguruPacks/internal/serverpacks"
	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
	updatepkg "github.com/MeguruMacabre/MeguruPacks/internal/update"
)

type ItemResult struct {
	PackName       string
	PackID         string
	LocalVersion   string
	RemoteVersion  string
	LocalManifest  string
	RemoteManifest string
	Updated        bool
	Skipped        bool
	ErrText        string
}

type Result struct {
	Checked int
	Updated int
	Skipped int
	Failed  int
	Items   []ItemResult
}

type StatusUpdate struct {
	Stage    string
	PackName string
	Current  int
	Total    int
	Done     bool
	Result   *Result
	ErrText  string
}

type installedPack struct {
	TargetDir string
	State     clientstate.State
}

func Run(
	ctx context.Context,
	store *storage.Client,
	installRoot string,
	statusCh chan<- StatusUpdate,
) Result {
	emit(statusCh, StatusUpdate{
		Stage: "Loading remote index",
	})

	remotePacks, err := serverpacks.ListFromIndex(ctx, store)
	if err != nil {
		res := Result{
			Failed: 1,
			Items: []ItemResult{
				{
					PackName: "startup",
					ErrText:  err.Error(),
				},
			},
		}
		emit(statusCh, StatusUpdate{
			Stage:   "Loading remote index",
			Done:    true,
			Result:  &res,
			ErrText: err.Error(),
		})
		return res
	}

	installed, err := findInstalledPacks(installRoot)
	if err != nil {
		res := Result{
			Failed: 1,
			Items: []ItemResult{
				{
					PackName: "startup",
					ErrText:  err.Error(),
				},
			},
		}
		emit(statusCh, StatusUpdate{
			Stage:   "Scanning installed packs",
			Done:    true,
			Result:  &res,
			ErrText: err.Error(),
		})
		return res
	}

	remoteByID := make(map[string]serverpacks.Pack, len(remotePacks))
	for _, p := range remotePacks {
		id := strings.TrimSpace(p.PackID)
		if id == "" {
			continue
		}
		remoteByID[id] = p
	}

	result := Result{
		Checked: len(installed),
		Items:   make([]ItemResult, 0, len(installed)),
	}

	total := len(installed)
	if total == 0 {
		emit(statusCh, StatusUpdate{
			Stage:  "No installed packs",
			Done:   true,
			Result: &result,
		})
		return result
	}

	for i, local := range installed {
		localID := strings.TrimSpace(local.State.PackID)
		localVersion := strings.TrimSpace(local.State.Version)
		if localVersion == "" {
			localVersion = "-"
		}
		localManifest := strings.TrimSpace(local.State.ManifestKey)

		remote, ok := remoteByID[localID]
		if !ok {
			result.Skipped++
			result.Items = append(result.Items, ItemResult{
				PackName:       local.State.PackName,
				PackID:         localID,
				LocalVersion:   localVersion,
				RemoteVersion:  "-",
				LocalManifest:  localManifest,
				RemoteManifest: "-",
				Skipped:        true,
			})
			emit(statusCh, StatusUpdate{
				Stage:    "Checking installed packs",
				PackName: local.State.PackName,
				Current:  i + 1,
				Total:    total,
			})
			continue
		}

		remoteVersion := strings.TrimSpace(remote.Version)
		if remoteVersion == "" {
			remoteVersion = "-"
		}
		remoteManifest := strings.TrimSpace(remote.ManifestKey)

		needsUpdate :=
			strings.TrimSpace(local.State.Version) != strings.TrimSpace(remote.Version) ||
				strings.TrimSpace(local.State.ManifestKey) != strings.TrimSpace(remote.ManifestKey)

		if !needsUpdate {
			result.Skipped++
			result.Items = append(result.Items, ItemResult{
				PackName:       remote.PackName,
				PackID:         remote.PackID,
				LocalVersion:   localVersion,
				RemoteVersion:  remoteVersion,
				LocalManifest:  localManifest,
				RemoteManifest: remoteManifest,
				Skipped:        true,
			})
			emit(statusCh, StatusUpdate{
				Stage:    "Checking installed packs",
				PackName: remote.PackName,
				Current:  i + 1,
				Total:    total,
			})
			continue
		}

		emit(statusCh, StatusUpdate{
			Stage:    "Updating installed packs",
			PackName: remote.PackName + " " + localVersion + " -> " + remoteVersion,
			Current:  i + 1,
			Total:    total,
		})

		_, updateErr := updatepkg.Pack(ctx, store, remote, installRoot, nil)
		if updateErr != nil {
			result.Failed++
			result.Items = append(result.Items, ItemResult{
				PackName:       remote.PackName,
				PackID:         remote.PackID,
				LocalVersion:   localVersion,
				RemoteVersion:  remoteVersion,
				LocalManifest:  localManifest,
				RemoteManifest: remoteManifest,
				ErrText:        updateErr.Error(),
			})
			continue
		}

		result.Updated++
		result.Items = append(result.Items, ItemResult{
			PackName:       remote.PackName,
			PackID:         remote.PackID,
			LocalVersion:   localVersion,
			RemoteVersion:  remoteVersion,
			LocalManifest:  localManifest,
			RemoteManifest: remoteManifest,
			Updated:        true,
		})
	}

	emit(statusCh, StatusUpdate{
		Stage:  "Startup sync finished",
		Done:   true,
		Result: &result,
	})
	return result
}

func findInstalledPacks(root string) ([]installedPack, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []installedPack{}, nil
		}
		return nil, err
	}

	result := make([]installedPack, 0)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		packDir := filepath.Join(root, entry.Name())
		state, exists, err := clientstate.Read(packDir)
		if err != nil {
			continue
		}
		if !exists {
			continue
		}

		result = append(result, installedPack{
			TargetDir: packDir,
			State:     state,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].State.PackName) < strings.ToLower(result[j].State.PackName)
	})

	return result, nil
}

func emit(ch chan<- StatusUpdate, update StatusUpdate) {
	if ch == nil {
		return
	}

	select {
	case ch <- update:
	default:
	}
}
