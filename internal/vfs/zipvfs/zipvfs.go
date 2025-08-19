package zipvfs

import (
	"archive/zip"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/microsoft/typescript-go/internal/tspath"
	"github.com/microsoft/typescript-go/internal/vfs"
	"github.com/microsoft/typescript-go/internal/vfs/iovfs"
)

type cachedZipReader struct {
	reader   *zip.ReadCloser
	lastUsed time.Time
	zipMTime time.Time
}

type zipFS struct {
	fs                  vfs.FS
	maxOpenReaders      int
	cachedZipReadersMap map[string]*cachedZipReader
	cacheReaderMutex    sync.Mutex
}

var _ vfs.FS = (*zipFS)(nil)

func From(fs vfs.FS) *zipFS {
	zipfs := &zipFS{
		fs:                  fs,
		maxOpenReaders:      80,
		cachedZipReadersMap: make(map[string]*cachedZipReader),
		cacheReaderMutex:    sync.Mutex{},
	}

	return zipfs
}

func (zipfs *zipFS) DirectoryExists(path string) bool {
	path, _, _ = resolveVirtual(path)

	if strings.HasSuffix(path, ".zip") {
		return zipfs.fs.FileExists(path)
	}

	fs, formattedPath, _ := getMatchingFS(zipfs, path)

	return fs.DirectoryExists(formattedPath)
}

func (zipfs *zipFS) FileExists(path string) bool {
	path, _, _ = resolveVirtual(path)

	if strings.HasSuffix(path, ".zip") {
		return zipfs.fs.FileExists(path)
	}

	fs, formattedPath, _ := getMatchingFS(zipfs, path)
	return fs.FileExists(formattedPath)
}

func (zipfs *zipFS) GetAccessibleEntries(path string) vfs.Entries {
	path, hash, basePath := resolveVirtual(path)

	fs, formattedPath, zipPath := getMatchingFS(zipfs, path)
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

func (zipfs *zipFS) ReadFile(path string) (contents string, ok bool) {
	path, _, _ = resolveVirtual(path)

	fs, formattedPath, _ := getMatchingFS(zipfs, path)
	return fs.ReadFile(formattedPath)
}

func (zipfs *zipFS) Realpath(path string) string {
	path, hash, basePath := resolveVirtual(path)

	fs, formattedPath, zipPath := getMatchingFS(zipfs, path)
	fullPath := filepath.Join(zipPath, fs.Realpath(formattedPath))
	return makeVirtualPath(basePath, hash, fullPath)
}

func (zipfs *zipFS) Remove(path string) error {
	path, _, _ = resolveVirtual(path)

	fs, formattedPath, _ := getMatchingFS(zipfs, path)
	return fs.Remove(formattedPath)
}

func (zipfs *zipFS) Stat(path string) vfs.FileInfo {
	path, _, _ = resolveVirtual(path)

	fs, formattedPath, _ := getMatchingFS(zipfs, path)
	return fs.Stat(formattedPath)
}

func (zipfs *zipFS) UseCaseSensitiveFileNames() bool {
	return zipfs.fs.UseCaseSensitiveFileNames()
}

func (zipfs *zipFS) WalkDir(root string, walkFn vfs.WalkDirFunc) error {
	root, hash, basePath := resolveVirtual(root)

	fs, formattedPath, zipPath := getMatchingFS(zipfs, root)
	return fs.WalkDir(formattedPath, (func(path string, d vfs.DirEntry, err error) error {
		fullPath := filepath.Join(zipPath, path)
		return walkFn(makeVirtualPath(basePath, hash, fullPath), d, err)
	}))
}

func (zipfs *zipFS) WriteFile(path string, data string, writeByteOrderMark bool) error {
	path, _, _ = resolveVirtual(path)

	fs, formattedPath, _ := getMatchingFS(zipfs, path)
	return fs.WriteFile(formattedPath, data, writeByteOrderMark)
}

func splitZipPath(path string) (string, string) {
	parts := strings.Split(path, ".zip/")
	if len(parts) < 2 {
		return path, "/"
	}
	return parts[0] + ".zip", "/" + parts[1]
}

func getMatchingFS(zipfs *zipFS, path string) (vfs.FS, string, string) {
	if !tspath.IsZipPath(path) {
		return zipfs.fs, path, ""
	}

	zipPath, internalPath := splitZipPath(path)

	zipStat := zipfs.fs.Stat(zipPath)
	if zipStat == nil {
		return zipfs.fs, path, ""
	}

	var usedReader *cachedZipReader

	zipfs.cacheReaderMutex.Lock()
	defer zipfs.cacheReaderMutex.Unlock()

	zipMTime := zipStat.ModTime()

	cachedReader, ok := zipfs.cachedZipReadersMap[zipPath]
	if ok && cachedReader.zipMTime.Equal(zipMTime) {
		cachedReader.lastUsed = time.Now()
		usedReader = cachedReader
	} else {
		zipReader, err := zip.OpenReader(zipPath)
		if err != nil {
			return zipfs.fs, path, ""
		}

		if len(zipfs.cachedZipReadersMap) >= zipfs.maxOpenReaders {
			zipfs.deleteOldestReader()
		}

		usedReader = &cachedZipReader{reader: zipReader, lastUsed: time.Now(), zipMTime: zipMTime}
		zipfs.cachedZipReadersMap[zipPath] = usedReader
	}

	return iovfs.From(usedReader.reader, zipfs.fs.UseCaseSensitiveFileNames()), internalPath, zipPath
}

func (zipfs *zipFS) deleteOldestReader() {
	var oldestReader *cachedZipReader
	var oldestReaderPath string
	for path, reader := range zipfs.cachedZipReadersMap {
		if oldestReader == nil || reader.lastUsed.Before(oldestReader.lastUsed) {
			oldestReader = reader
			oldestReaderPath = path
		}
	}

	if oldestReader != nil {
		oldestReader.reader.Close()
		delete(zipfs.cachedZipReadersMap, oldestReaderPath)
	}
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
