package packcleanup

import (
	"context"
	"strings"

	gcsvc "github.com/MeguruMacabre/MeguruPacks/internal/gc"
	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
)

type Result struct {
	DeletedManifestKeys int
	GCResult            gcsvc.Result
}

func AfterPublish(ctx context.Context, store *storage.Client, packID string, keepManifestFullKey string) (Result, error) {
	packID = strings.TrimSpace(packID)
	keepManifestFullKey = strings.TrimSpace(keepManifestFullKey)

	if packID == "" {
		return Result{}, nil
	}

	manifestKeys, err := store.ListFullKeysByPrefix(ctx, "packs/"+packID+"/manifests")
	if err != nil {
		return Result{}, err
	}

	toDelete := make([]string, 0, len(manifestKeys))
	for _, key := range manifestKeys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if key == keepManifestFullKey {
			continue
		}
		toDelete = append(toDelete, key)
	}

	if err := store.DeleteFullKeys(ctx, toDelete); err != nil {
		return Result{}, err
	}

	gcRes, err := gcsvc.Sweep(ctx, store)
	if err != nil {
		return Result{}, err
	}

	return Result{
		DeletedManifestKeys: len(toDelete),
		GCResult:            gcRes,
	}, nil
}
