package zipvfs

import (
	"archive/zip"
	"path/filepath"
	"strings"

	"github.com/microsoft/typescript-go/internal/vfs"
	"github.com/microsoft/typescript-go/internal/vfs/iovfs"
)

type FS struct {
	fs vfs.FS
}

var _ vfs.FS = (*FS)(nil)

func From(fs vfs.FS) *FS {
	fsys := &FS{fs: fs}
	return fsys
}

func (fsys *FS) DirectoryExists(path string) bool {
	fs, formattedPath, _ := getMatchingFS(fsys, path)
	return fs.DirectoryExists(formattedPath)
}

func (fsys *FS) FileExists(path string) bool {
	if strings.HasSuffix(path, ".zip") {
		return fsys.fs.FileExists(path)
	}

	fs, formattedPath, _ := getMatchingFS(fsys, path)
	return fs.FileExists(formattedPath)
}

func (fsys *FS) GetAccessibleEntries(path string) vfs.Entries {
	fs, formattedPath, zipPath := getMatchingFS(fsys, path)
	entries := fs.GetAccessibleEntries(formattedPath)

	for i, dir := range entries.Directories {
		entries.Directories[i] = filepath.Join(zipPath, dir)
	}

	for i, file := range entries.Files {
		entries.Files[i] = filepath.Join(zipPath, file)
	}

	return entries
}

func (fsys *FS) ReadFile(path string) (contents string, ok bool) {
	fs, formattedPath, _ := getMatchingFS(fsys, path)
	return fs.ReadFile(formattedPath)
}

func (fsys *FS) Realpath(path string) string {
	fs, formattedPath, zipPath := getMatchingFS(fsys, path)
	return filepath.Join(zipPath, fs.Realpath(formattedPath))
}

func (fsys *FS) Remove(path string) error {
	fs, formattedPath, _ := getMatchingFS(fsys, path)
	return fs.Remove(formattedPath)
}

func (fsys *FS) Stat(path string) vfs.FileInfo {
	fs, formattedPath, _ := getMatchingFS(fsys, path)
	return fs.Stat(formattedPath)
}

func (fsys *FS) UseCaseSensitiveFileNames() bool {
	return fsys.fs.UseCaseSensitiveFileNames()
}

func (fsys *FS) WalkDir(root string, walkFn vfs.WalkDirFunc) error {
	fs, formattedPath, zipPath := getMatchingFS(fsys, root)
	return fs.WalkDir(formattedPath, (func(path string, d vfs.DirEntry, err error) error {
		return walkFn(filepath.Join(zipPath, path), d, err)
	}))
}

func (fsys *FS) WriteFile(path string, data string, writeByteOrderMark bool) error {
	fs, formattedPath, _ := getMatchingFS(fsys, path)
	return fs.WriteFile(formattedPath, data, writeByteOrderMark)
}

func isZipPath(path string) bool {
	return strings.Contains(path, ".zip/") || strings.HasSuffix(path, ".zip")
}

func splitZipPath(path string) (string, string) {
	parts := strings.Split(path, ".zip/")
	if len(parts) < 2 {
		return path, "/"
	}
	return parts[0] + ".zip", "/" + parts[1]
}

func getMatchingFS(fsys *FS, path string) (vfs.FS, string, string) {
	if !isZipPath(path) {
		return fsys.fs, path, ""
	}

	zipPath, internalPath := splitZipPath(path)
	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fsys.fs, path, ""
	}

	return iovfs.From(zipReader, fsys.fs.UseCaseSensitiveFileNames()), internalPath, zipPath
}
