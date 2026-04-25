package manifest

import "time"

type FileEntry struct {
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"size_bytes"`
	SHA256     string    `json:"sha256"`
	ModifiedAt time.Time `json:"modified_at"`
}

type Manifest struct {
	PackName    string      `json:"pack_name"`
	PackID      string      `json:"pack_id"`
	Version     string      `json:"version"`
	GeneratedAt time.Time   `json:"generated_at"`
	FileCount   int         `json:"file_count"`
	SizeBytes   int64       `json:"size_bytes"`
	Files       []FileEntry `json:"files"`
}
