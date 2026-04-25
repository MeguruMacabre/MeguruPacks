package gc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MeguruMacabre/MeguruPacks/internal/manifest"
	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
)

type Result struct {
	ManifestKeysScanned int
	ReferencedObjects   int
	ObjectsScanned      int
	DeletedObjects      int
	DeletedBytes        int64
	CompletedAt         time.Time
}

func Sweep(ctx context.Context, store *storage.Client) (Result, error) {
	packObjects, err := store.ListObjectsByPrefix(ctx, "packs")
	if err != nil {
		return Result{}, err
	}

	manifestKeys := make([]string, 0)
	for _, obj := range packObjects {
		if isManifestKey(obj.Key) {
			manifestKeys = append(manifestKeys, obj.Key)
		}
	}

	referenced := make(map[string]struct{})

	for _, manifestKey := range manifestKeys {
		data, err := store.ReadFullKeyBytes(ctx, manifestKey)
		if err != nil {
			return Result{}, fmt.Errorf("read manifest %s: %w", manifestKey, err)
		}

		var mf manifest.Manifest
		if err := json.Unmarshal(data, &mf); err != nil {
			return Result{}, fmt.Errorf("parse manifest %s: %w", manifestKey, err)
		}

		for _, file := range mf.Files {
			objectKey := store.FullKey(objectKeyForHash(file.SHA256))
			referenced[objectKey] = struct{}{}
		}
	}

	objectInfos, err := store.ListObjectsByPrefix(ctx, "objects")
	if err != nil {
		return Result{}, err
	}

	toDelete := make([]string, 0)
	var deletedBytes int64

	for _, obj := range objectInfos {
		if _, ok := referenced[obj.Key]; ok {
			continue
		}

		toDelete = append(toDelete, obj.Key)
		deletedBytes += obj.Size
	}

	if err := store.DeleteFullKeys(ctx, toDelete); err != nil {
		return Result{}, err
	}

	return Result{
		ManifestKeysScanned: len(manifestKeys),
		ReferencedObjects:   len(referenced),
		ObjectsScanned:      len(objectInfos),
		DeletedObjects:      len(toDelete),
		DeletedBytes:        deletedBytes,
		CompletedAt:         time.Now().UTC(),
	}, nil
}

func isManifestKey(fullKey string) bool {
	return strings.Contains(fullKey, "/manifests/") && strings.HasSuffix(fullKey, ".json")
}

func objectKeyForHash(hash string) string {
	if len(hash) < 4 {
		return "objects/" + hash
	}
	return fmt.Sprintf("objects/%s/%s/%s", hash[:2], hash[2:4], hash)
}
