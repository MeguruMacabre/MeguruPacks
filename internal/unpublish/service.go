package unpublish

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
)

type Result struct {
	PackName    string
	PackID      string
	PackSlug    string
	DeletedKeys int
	Prefix      string
	DeletedAt   time.Time
}

func PackByID(ctx context.Context, store *storage.Client, packID, packName, packSlug string) (Result, error) {
	packID = strings.TrimSpace(packID)
	if packID == "" {
		return Result{}, errors.New("packID is empty")
	}

	relativePrefix := fmt.Sprintf("packs/%s/", packID)

	fullKeys, err := store.ListFullKeysByPrefix(ctx, relativePrefix)
	if err != nil {
		return Result{}, err
	}

	if err := store.DeleteFullKeys(ctx, fullKeys); err != nil {
		return Result{}, err
	}

	return Result{
		PackName:    packName,
		PackID:      packID,
		PackSlug:    packSlug,
		DeletedKeys: len(fullKeys),
		Prefix:      store.FullKey(relativePrefix),
		DeletedAt:   time.Now().UTC(),
	}, nil
}
