package publish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MeguruMacabre/MeguruPacks/internal/manifest"
	"github.com/MeguruMacabre/MeguruPacks/internal/packid"
	"github.com/MeguruMacabre/MeguruPacks/internal/packmeta"
	"github.com/MeguruMacabre/MeguruPacks/internal/packs"
	"github.com/MeguruMacabre/MeguruPacks/internal/storage"
)

type Result struct {
	PackName         string
	PackID           string
	PackSlug         string
	Version          string
	ManifestKey      string
	LatestKey        string
	UploadedObjects  int
	SkippedObjects   int
	PublishedAt      time.Time
	ManifestFileSize int
}

type Latest struct {
	PackName    string    `json:"pack_name"`
	PackID      string    `json:"pack_id"`
	PackSlug    string    `json:"pack_slug"`
	Version     string    `json:"version"`
	ManifestKey string    `json:"manifest_key"`
	PublishedAt time.Time `json:"published_at"`
	FileCount   int       `json:"file_count"`
	SizeBytes   int64     `json:"size_bytes"`
}

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

func PackWithProgress(
	ctx context.Context,
	store *storage.Client,
	pack packs.Pack,
	progressCh chan<- ProgressUpdate,
) (Result, error) {
	meta, err := packmeta.Ensure(pack.Path, pack.FolderName)
	if err != nil {
		return Result{}, err
	}

	if strings.TrimSpace(pack.Name) == "" {
		pack.Name = packmeta.VisibleName(meta, pack.FolderName)
	}
	pack.PackID = meta.PackID
	if strings.TrimSpace(pack.Version) == "" {
		pack.Version = packmeta.VisibleVersion(meta)
	}

	packSlug := packid.Slug(pack.Name)

	m, err := buildManifestWithProgress(pack, progressCh)
	if err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Hashing",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	pendingUploads := make([]manifest.FileEntry, 0, len(m.Files))
	var uploadBytesTotal int64
	skipped := 0

	for i, file := range m.Files {
		objectKey := objectKeyForHash(file.SHA256)

		exists, err := store.ObjectExists(ctx, objectKey)
		if err != nil {
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Checking objects",
				CurrentFile: file.Path,
				FilesDone:   i,
				FilesTotal:  len(m.Files),
				Done:        true,
				Err:         err,
			})
			return Result{}, fmt.Errorf("head object %s: %w", objectKey, err)
		}

		if exists {
			skipped++
		} else {
			pendingUploads = append(pendingUploads, file)
			uploadBytesTotal += file.SizeBytes
		}

		emitProgress(progressCh, ProgressUpdate{
			Stage:       "Checking objects",
			CurrentFile: file.Path,
			FilesDone:   i + 1,
			FilesTotal:  len(m.Files),
		})
	}

	uploaded := 0
	var uploadedBytes int64

	for i, file := range pendingUploads {
		objectKey := objectKeyForHash(file.SHA256)
		fullPath := filepath.Join(pack.Path, filepath.FromSlash(file.Path))

		f, err := os.Open(fullPath)
		if err != nil {
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Uploading",
				CurrentFile: file.Path,
				FilesDone:   i,
				FilesTotal:  len(pendingUploads),
				BytesDone:   uploadedBytes,
				BytesTotal:  uploadBytesTotal,
				Done:        true,
				Err:         err,
			})
			return Result{}, err
		}

		reader := &progressReader{
			r: f,
			onRead: func(n int64) {
				uploadedBytes += n
				emitProgress(progressCh, ProgressUpdate{
					Stage:       "Uploading",
					CurrentFile: file.Path,
					FilesDone:   i,
					FilesTotal:  len(pendingUploads),
					BytesDone:   uploadedBytes,
					BytesTotal:  uploadBytesTotal,
				})
			},
		}

		err = store.UploadReader(ctx, objectKey, reader, file.SizeBytes, detectContentType(file.Path))
		_ = f.Close()

		if err != nil {
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Uploading",
				CurrentFile: file.Path,
				FilesDone:   i,
				FilesTotal:  len(pendingUploads),
				BytesDone:   uploadedBytes,
				BytesTotal:  uploadBytesTotal,
				Done:        true,
				Err:         err,
			})
			return Result{}, fmt.Errorf("upload object %s: %w", objectKey, err)
		}

		uploaded++
		emitProgress(progressCh, ProgressUpdate{
			Stage:       "Uploading",
			CurrentFile: file.Path,
			FilesDone:   uploaded,
			FilesTotal:  len(pendingUploads),
			BytesDone:   uploadedBytes,
			BytesTotal:  uploadBytesTotal,
		})
	}

	now := time.Now().UTC()
	timestamp := fmt.Sprintf("%s-%d", now.Format("20060102-150405"), now.UnixNano())

	manifestKey := fmt.Sprintf("packs/%s/manifests/%s.json", pack.PackID, timestamp)
	latestKey := fmt.Sprintf("packs/%s/latest.json", pack.PackID)

	manifestBytes, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Publishing metadata",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	emitProgress(progressCh, ProgressUpdate{
		Stage:       "Publishing metadata",
		CurrentFile: "manifest.json",
		FilesDone:   0,
		FilesTotal:  2,
	})

	if err := store.UploadBytes(ctx, manifestKey, manifestBytes, "application/json"); err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage:       "Publishing metadata",
			CurrentFile: "manifest.json",
			FilesDone:   0,
			FilesTotal:  2,
			Done:        true,
			Err:         err,
		})
		return Result{}, fmt.Errorf("upload manifest: %w", err)
	}

	latest := Latest{
		PackName:    pack.Name,
		PackID:      pack.PackID,
		PackSlug:    packSlug,
		Version:     pack.Version,
		ManifestKey: store.FullKey(manifestKey),
		PublishedAt: now,
		FileCount:   m.FileCount,
		SizeBytes:   m.SizeBytes,
	}

	latestBytes, err := json.MarshalIndent(latest, "", "  ")
	if err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage: "Publishing metadata",
			Done:  true,
			Err:   err,
		})
		return Result{}, err
	}

	emitProgress(progressCh, ProgressUpdate{
		Stage:       "Publishing metadata",
		CurrentFile: "latest.json",
		FilesDone:   1,
		FilesTotal:  2,
	})

	if err := store.UploadBytes(ctx, latestKey, latestBytes, "application/json"); err != nil {
		emitProgress(progressCh, ProgressUpdate{
			Stage:       "Publishing metadata",
			CurrentFile: "latest.json",
			FilesDone:   1,
			FilesTotal:  2,
			Done:        true,
			Err:         err,
		})
		return Result{}, fmt.Errorf("upload latest: %w", err)
	}

	emitProgress(progressCh, ProgressUpdate{
		Stage:      "Done",
		FilesDone:  2,
		FilesTotal: 2,
		BytesDone:  uploadBytesTotal,
		BytesTotal: uploadBytesTotal,
		Done:       true,
	})

	return Result{
		PackName:         pack.Name,
		PackID:           pack.PackID,
		PackSlug:         packSlug,
		Version:          pack.Version,
		ManifestKey:      store.FullKey(manifestKey),
		LatestKey:        store.FullKey(latestKey),
		UploadedObjects:  uploaded,
		SkippedObjects:   skipped,
		PublishedAt:      now,
		ManifestFileSize: len(manifestBytes),
	}, nil
}

func buildManifestWithProgress(pack packs.Pack, progressCh chan<- ProgressUpdate) (manifest.Manifest, error) {
	result := manifest.Manifest{
		PackName:    pack.Name,
		PackID:      pack.PackID,
		Version:     pack.Version,
		GeneratedAt: time.Now(),
		FileCount:   pack.FileCount,
		SizeBytes:   pack.SizeBytes,
		Files:       make([]manifest.FileEntry, 0, len(pack.Files)),
	}

	var hashedBytes int64

	for i, file := range pack.Files {
		fullPath := filepath.Join(pack.Path, filepath.FromSlash(file.RelativePath))

		hash, err := fileSHA256WithProgress(fullPath, func(n int64) {
			hashedBytes += n
			emitProgress(progressCh, ProgressUpdate{
				Stage:       "Hashing",
				CurrentFile: file.RelativePath,
				FilesDone:   i,
				FilesTotal:  len(pack.Files),
				BytesDone:   hashedBytes,
				BytesTotal:  pack.SizeBytes,
			})
		})
		if err != nil {
			return manifest.Manifest{}, err
		}

		result.Files = append(result.Files, manifest.FileEntry{
			Path:       file.RelativePath,
			SizeBytes:  file.SizeBytes,
			SHA256:     hash,
			ModifiedAt: file.ModifiedAt,
		})

		emitProgress(progressCh, ProgressUpdate{
			Stage:       "Hashing",
			CurrentFile: file.RelativePath,
			FilesDone:   i + 1,
			FilesTotal:  len(pack.Files),
			BytesDone:   hashedBytes,
			BytesTotal:  pack.SizeBytes,
		})
	}

	sort.Slice(result.Files, func(i, j int) bool {
		return result.Files[i].Path < result.Files[j].Path
	})

	return result, nil
}

func fileSHA256WithProgress(path string, onRead func(int64)) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	buf := make([]byte, 256*1024)

	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, writeErr := h.Write(buf[:n]); writeErr != nil {
				return "", writeErr
			}
			if onRead != nil {
				onRead(int64(n))
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

type progressReader struct {
	r      io.Reader
	onRead func(int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 && pr.onRead != nil {
		pr.onRead(int64(n))
	}
	return n, err
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

func objectKeyForHash(hash string) string {
	if len(hash) < 4 {
		return "objects/" + hash
	}
	return fmt.Sprintf("objects/%s/%s/%s", hash[:2], hash[2:4], hash)
}

func detectContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".json":
		return "application/json"
	case ".txt", ".cfg", ".log", ".md":
		return "text/plain; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".zip":
		return "application/zip"
	case ".jar":
		return "application/java-archive"
	default:
		return "application/octet-stream"
	}
}
