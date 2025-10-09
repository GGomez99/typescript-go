package pnpvfs_test

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/tspath"
	"github.com/microsoft/typescript-go/internal/vfs/osvfs"
	"github.com/microsoft/typescript-go/internal/vfs/pnpvfs"
	"github.com/microsoft/typescript-go/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func createTestZip(t *testing.T, files map[string]string) string {
	t.Helper()

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "test.zip")

	file, err := os.Create(zipPath)
	assert.NilError(t, err)
	defer file.Close()

	w := zip.NewWriter(file)
	defer w.Close()

	for name, content := range files {
		f, err := w.Create(name)
		assert.NilError(t, err)
		_, err = f.Write([]byte(content))
		assert.NilError(t, err)
	}

	return zipPath
}

func TestPnpVfs_BasicFileOperations(t *testing.T) {
	t.Parallel()

	underlyingFS := vfstest.FromMap(map[string]string{
		"/project/src/index.ts": "export const hello = 'world';",
		"/project/package.json": `{"name": "test"}`,
	}, true)

	fs := pnpvfs.From(underlyingFS)
	assert.Assert(t, fs.FileExists("/project/src/index.ts"))
	assert.Assert(t, !fs.FileExists("/project/nonexistent.ts"))

	content, ok := fs.ReadFile("/project/src/index.ts")
	assert.Assert(t, ok)
	assert.Equal(t, "export const hello = 'world';", content)

	assert.Assert(t, fs.DirectoryExists("/project/src"))
	assert.Assert(t, !fs.DirectoryExists("/project/nonexistent"))
}

func TestPnpVfs_ZipFileDetection(t *testing.T) {
	t.Parallel()

	zipFiles := map[string]string{
		"src/index.ts": "export const hello = 'world';",
		"package.json": `{"name": "test-project"}`,
	}

	zipPath := createTestZip(t, zipFiles)

	underlyingFS := vfstest.FromMap(map[string]string{
		zipPath: "zip content placeholder",
	}, true)

	fs := pnpvfs.From(underlyingFS)

	fmt.Println(zipPath)
	assert.Assert(t, fs.FileExists(zipPath))

	zipInternalPath := zipPath + "/src/index.ts"
	assert.Assert(t, fs.FileExists(zipInternalPath))

	content, ok := fs.ReadFile(zipInternalPath)
	assert.Assert(t, ok)
	assert.Equal(t, content, zipFiles["src/index.ts"])
}

func TestPnpVfs_ErrorHandling(t *testing.T) {
	t.Parallel()

	fs := pnpvfs.From(osvfs.FS())

	t.Run("NonexistentZipFile", func(t *testing.T) {
		result := fs.FileExists("/nonexistent/path/archive.zip/file.txt")
		assert.Assert(t, !result)

		_, ok := fs.ReadFile("/nonexistent/archive.zip/file.txt")
		assert.Assert(t, !ok)
	})

	t.Run("InvalidZipFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		fakePath := filepath.Join(tmpDir, "fake.zip")
		err := os.WriteFile(fakePath, []byte("not a zip file"), 0o644)
		assert.NilError(t, err)
		result := fs.FileExists(fakePath + "/file.txt")
		assert.Assert(t, !result)
	})
}

func TestPnpVfs_CaseSensitivity(t *testing.T) {
	t.Parallel()

	sensitiveFS := pnpvfs.From(vfstest.FromMap(map[string]string{}, true))
	assert.Assert(t, sensitiveFS.UseCaseSensitiveFileNames())
	insensitiveFS := pnpvfs.From(vfstest.FromMap(map[string]string{}, false))
	// pnpvfs is always case sensitive
	assert.Assert(t, insensitiveFS.UseCaseSensitiveFileNames())
}

func TestPnpVfs_FallbackToRegularFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	regularFile := filepath.Join(tmpDir, "regular.ts")
	err := os.WriteFile(regularFile, []byte("regular content"), 0o644)
	assert.NilError(t, err)

	fs := pnpvfs.From(osvfs.FS())

	assert.Assert(t, fs.FileExists(regularFile))

	content, ok := fs.ReadFile(regularFile)
	assert.Assert(t, ok)
	assert.Equal(t, "regular content", content)
	assert.Assert(t, fs.DirectoryExists(tmpDir))
}

func TestZipPath_Detection(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		path        string
		shouldBeZip bool
	}{
		{"/normal/path/file.txt", false},
		{"/path/to/archive.zip", true},
		{"/path/to/archive.zip/internal/file.txt", true},
		{"/path/archive.zip/nested/dir/file.ts", true},
		{"/path/file.zip.txt", false},
		{"/absolute/archive.zip", true},
		{"/absolute/archive.zip/file.txt", true},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			assert.Assert(t, tspath.IsZipPath(tc.path) == tc.shouldBeZip)
		})
	}
}

func TestPnpVfs_RealZipIntegration(t *testing.T) {
	t.Parallel()

	zipFiles := map[string]string{
		"src/index.ts":         "export const hello = 'world';",
		"src/utils/helpers.ts": "export function add(a: number, b: number) { return a + b; }",
		"package.json":         `{"name": "test-project", "version": "1.0.0"}`,
		"tsconfig.json":        `{"compilerOptions": {"target": "es2020"}}`,
	}

	zipPath := createTestZip(t, zipFiles)
	fs := pnpvfs.From(osvfs.FS())

	assert.Assert(t, fs.FileExists(zipPath))

	indexPath := zipPath + "/src/index.ts"
	packagePath := zipPath + "/package.json"
	assert.Assert(t, fs.FileExists(indexPath))
	assert.Assert(t, fs.FileExists(packagePath))
	assert.Assert(t, fs.DirectoryExists(zipPath+"/src"))

	content, ok := fs.ReadFile(indexPath)
	assert.Assert(t, ok)
	assert.Equal(t, content, zipFiles["src/index.ts"])

	content, ok = fs.ReadFile(packagePath)
	assert.Assert(t, ok)
	assert.Equal(t, content, zipFiles["package.json"])

	entries := fs.GetAccessibleEntries(zipPath)
	assert.DeepEqual(t, entries.Files, []string{zipPath + "/package.json", zipPath + "/tsconfig.json"})
	assert.DeepEqual(t, entries.Directories, []string{zipPath + "/src"})

	entries = fs.GetAccessibleEntries(zipPath + "/src")
	assert.DeepEqual(t, entries.Files, []string{zipPath + "/src/index.ts"})
	assert.DeepEqual(t, entries.Directories, []string{zipPath + "/src/utils"})

	assert.Equal(t, fs.Realpath(indexPath), indexPath)
}
