package serverpacks

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/MeguruMacabre/MeguruPacks/internal/manifest"
	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
)

type Pack struct {
	PackName    string    `json:"pack_name"`
	PackID      string    `json:"pack_id"`
	PackSlug    string    `json:"pack_slug"`
	Version     string    `json:"version"`
	ManifestKey string    `json:"manifest_key"`
	LatestKey   string    `json:"latest_key"`
	PublishedAt time.Time `json:"published_at"`
	FileCount   int       `json:"file_count"`
	SizeBytes   int64     `json:"size_bytes"`
}

type HistoryEntry struct {
	PackName    string    `json:"pack_name"`
	PackID      string    `json:"pack_id"`
	Version     string    `json:"version"`
	ManifestKey string    `json:"manifest_key"`
	GeneratedAt time.Time `json:"generated_at"`
	FileCount   int       `json:"file_count"`
	SizeBytes   int64     `json:"size_bytes"`
}

type latestJSON struct {
	PackName    string    `json:"pack_name"`
	PackID      string    `json:"pack_id"`
	PackSlug    string    `json:"pack_slug"`
	Version     string    `json:"version"`
	ManifestKey string    `json:"manifest_key"`
	PublishedAt time.Time `json:"published_at"`
	FileCount   int       `json:"file_count"`
	SizeBytes   int64     `json:"size_bytes"`
}

type publicIndexJSON struct {
	GeneratedAt time.Time `json:"generated_at"`
	Packs       []Pack    `json:"packs"`
}

func List(ctx context.Context, store *storage.Client) ([]Pack, error) {
	objects, err := store.ListObjectsByPrefix(ctx, "packs")
	if err != nil {
		return nil, err
	}

	result := make([]Pack, 0)

	for _, obj := range objects {
		if !strings.HasSuffix(obj.Key, "/latest.json") {
			continue
		}

		data, err := store.ReadFullKeyBytes(ctx, obj.Key)
		if err != nil {
			return nil, err
		}

		var latest latestJSON
		if err := json.Unmarshal(data, &latest); err != nil {
			return nil, err
		}

		if strings.TrimSpace(latest.PackID) == "" {
			latest.PackID = derivePackIDFromLatestKey(obj.Key)
		}
		if strings.TrimSpace(latest.PackName) == "" {
			latest.PackName = latest.PackID
		}

		result = append(result, Pack{
			PackName:    latest.PackName,
			PackID:      latest.PackID,
			PackSlug:    latest.PackSlug,
			Version:     latest.Version,
			ManifestKey: latest.ManifestKey,
			LatestKey:   obj.Key,
			PublishedAt: latest.PublishedAt,
			FileCount:   latest.FileCount,
			SizeBytes:   latest.SizeBytes,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].PublishedAt.Equal(result[j].PublishedAt) {
			return strings.ToLower(result[i].PackName) < strings.ToLower(result[j].PackName)
		}
		return result[i].PublishedAt.After(result[j].PublishedAt)
	})

	return result, nil
}

func ListFromIndex(ctx context.Context, store *storage.Client) ([]Pack, error) {
	data, err := store.ReadRelativeKeyBytes(ctx, "index.json")
	if err != nil {
		return nil, err
	}

	var idx publicIndexJSON
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}

	if idx.Packs == nil {
		return []Pack{}, nil
	}

	sort.Slice(idx.Packs, func(i, j int) bool {
		if idx.Packs[i].PublishedAt.Equal(idx.Packs[j].PublishedAt) {
			return strings.ToLower(idx.Packs[i].PackName) < strings.ToLower(idx.Packs[j].PackName)
		}
		return idx.Packs[i].PublishedAt.After(idx.Packs[j].PublishedAt)
	})

	return idx.Packs, nil
}

func ListHistory(ctx context.Context, store *storage.Client, packID string) ([]HistoryEntry, error) {
	packID = strings.TrimSpace(packID)
	if packID == "" {
		return []HistoryEntry{}, nil
	}

	objects, err := store.ListObjectsByPrefix(ctx, "packs/"+packID+"/manifests")
	if err != nil {
		return nil, err
	}

	result := make([]HistoryEntry, 0)

	for _, obj := range objects {
		if !strings.HasSuffix(obj.Key, ".json") {
			continue
		}

		data, err := store.ReadFullKeyBytes(ctx, obj.Key)
		if err != nil {
			return nil, err
		}

		var mf manifest.Manifest
		if err := json.Unmarshal(data, &mf); err != nil {
			return nil, err
		}

		packName := strings.TrimSpace(mf.PackName)
		if packName == "" {
			packName = packID
		}

		result = append(result, HistoryEntry{
			PackName:    packName,
			PackID:      nonEmpty(mf.PackID, packID),
			Version:     mf.Version,
			ManifestKey: obj.Key,
			GeneratedAt: mf.GeneratedAt,
			FileCount:   mf.FileCount,
			SizeBytes:   mf.SizeBytes,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].GeneratedAt.Equal(result[j].GeneratedAt) {
			return result[i].ManifestKey > result[j].ManifestKey
		}
		return result[i].GeneratedAt.After(result[j].GeneratedAt)
	})

	return result, nil
}

func derivePackIDFromLatestKey(fullKey string) string {
	parts := strings.Split(fullKey, "/")
	for i := 0; i < len(parts)-2; i++ {
		if parts[i] == "packs" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}
