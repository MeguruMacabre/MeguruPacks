package publicindex

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/MeguruMacabre/MeguruPacks/internal/serverpacks"
	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
)

const DefaultIndexKey = "index.json"

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

type Index struct {
	GeneratedAt time.Time `json:"generated_at"`
	Packs       []Pack    `json:"packs"`
}

func Build(ctx context.Context, store *storage.Client) (Index, error) {
	packs, err := serverpacks.List(ctx, store)
	if err != nil {
		return Index{}, err
	}

	result := make([]Pack, 0, len(packs))
	for _, p := range packs {
		result = append(result, Pack{
			PackName:    p.PackName,
			PackID:      p.PackID,
			PackSlug:    p.PackSlug,
			Version:     p.Version,
			ManifestKey: p.ManifestKey,
			LatestKey:   p.LatestKey,
			PublishedAt: p.PublishedAt,
			FileCount:   p.FileCount,
			SizeBytes:   p.SizeBytes,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].PublishedAt.Equal(result[j].PublishedAt) {
			return strings.ToLower(result[i].PackName) < strings.ToLower(result[j].PackName)
		}
		return result[i].PublishedAt.After(result[j].PublishedAt)
	})

	return Index{
		GeneratedAt: time.Now().UTC(),
		Packs:       result,
	}, nil
}

func Publish(ctx context.Context, store *storage.Client) error {
	idx, err := Build(ctx, store)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}

	return store.UploadBytes(ctx, DefaultIndexKey, data, "application/json")
}
