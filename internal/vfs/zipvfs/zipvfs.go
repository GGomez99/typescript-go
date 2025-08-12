package zipvfs

import (
	"archive/zip"
	"path"
	"path/filepath"
	"strconv"
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
	path, _, _ = resolveVirtual(path)

	if strings.HasSuffix(path, ".zip") {
		return fsys.fs.FileExists(path)
	}

	fs, formattedPath, _ := getMatchingFS(fsys, path)

	return fs.DirectoryExists(formattedPath)
}

func (fsys *FS) FileExists(path string) bool {
	path, _, _ = resolveVirtual(path)

	if strings.HasSuffix(path, ".zip") {
		return fsys.fs.FileExists(path)
	}

	fs, formattedPath, _ := getMatchingFS(fsys, path)
	return fs.FileExists(formattedPath)
}

func (fsys *FS) GetAccessibleEntries(path string) vfs.Entries {
	path, hash, basePath := resolveVirtual(path)

	fs, formattedPath, zipPath := getMatchingFS(fsys, path)
	entries := fs.GetAccessibleEntries(formattedPath)

	for i, dir := range entries.Directories {
		fullPath := filepath.Join(zipPath, dir)
		entries.Directories[i] = makeVirtualPath(basePath, hash, fullPath)
	}

	for i, file := range entries.Files {
		fullPath := filepath.Join(zipPath, file)
		entries.Files[i] = makeVirtualPath(basePath, hash, fullPath)
	}

	return entries
}

func (fsys *FS) ReadFile(path string) (contents string, ok bool) {
	path, _, _ = resolveVirtual(path)

	fs, formattedPath, _ := getMatchingFS(fsys, path)
	return fs.ReadFile(formattedPath)
}

func (fsys *FS) Realpath(path string) string {
	path, hash, basePath := resolveVirtual(path)

	fs, formattedPath, zipPath := getMatchingFS(fsys, path)
	fullPath := filepath.Join(zipPath, fs.Realpath(formattedPath))
	return makeVirtualPath(basePath, hash, fullPath)
}

func (fsys *FS) Remove(path string) error {
	path, _, _ = resolveVirtual(path)

	fs, formattedPath, _ := getMatchingFS(fsys, path)
	return fs.Remove(formattedPath)
}

func (fsys *FS) Stat(path string) vfs.FileInfo {
	path, _, _ = resolveVirtual(path)

	fs, formattedPath, _ := getMatchingFS(fsys, path)
	return fs.Stat(formattedPath)
}

func (fsys *FS) UseCaseSensitiveFileNames() bool {
	return fsys.fs.UseCaseSensitiveFileNames()
}

func (fsys *FS) WalkDir(root string, walkFn vfs.WalkDirFunc) error {
	root, hash, basePath := resolveVirtual(root)

	fs, formattedPath, zipPath := getMatchingFS(fsys, root)
	return fs.WalkDir(formattedPath, (func(path string, d vfs.DirEntry, err error) error {
		fullPath := filepath.Join(zipPath, path)
		return walkFn(makeVirtualPath(basePath, hash, fullPath), d, err)
	}))
}

func (fsys *FS) WriteFile(path string, data string, writeByteOrderMark bool) error {
	path, _, _ = resolveVirtual(path)

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

// TODO insert virtual path handling more properly (with a vfs wrapper maybe)
func resolveVirtual(path string) (realPath string, hash string, basePath string) {
	idx := strings.Index(path, "/__virtual__/")
	if idx == -1 {
		return path, "", ""
	}

	base := path[:idx]
	rest := path[idx+len("/__virtual__/"):]
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 3 {
		// Not enough parts to match the pattern, return as is
		return path, "", ""
	}
	hash = parts[0]
	subpath := parts[2]
	depth, err := strconv.Atoi(parts[1])
	if err != nil || depth < 0 {
		// Invalid n, return as is
		return path, "", ""
	}

	basePath = path[:idx] + "/__virtual__"

	// Apply dirname n times to base
	for i := 0; i < depth; i++ {
		base = filepath.Dir(base)
	}
	// Join base and subpath
	if base == "/" {
		return "/" + subpath, hash, basePath
	}

	return filepath.Join(base, subpath), hash, basePath
}

func makeVirtualPath(basePath string, hash string, targetPath string) string {
	if basePath == "" || hash == "" {
		return targetPath
	}

	relativePath, err := filepath.Rel(path.Dir(basePath), targetPath)
	if err != nil {
		panic("Could not make virtual path: " + err.Error())
	}

	segments := strings.Split(relativePath, "/")

	depth := 0
	for depth < len(segments) && segments[depth] == ".." {
		depth++
	}

	subPath := strings.Join(segments[depth:], "/")

	return path.Join(basePath, hash, strconv.Itoa(depth), subPath)
}
