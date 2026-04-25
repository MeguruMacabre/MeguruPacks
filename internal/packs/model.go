package packs

import "time"

type FileEntry struct {
	RelativePath string
	SizeBytes    int64
	ModifiedAt   time.Time
}

type Pack struct {
	Name         string
	FolderName   string
	Path         string
	PackID       string
	Version      string
	SizeBytes    int64
	FileCount    int
	LastModified time.Time
	Files        []FileEntry
}
