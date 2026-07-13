package module

import (
	"testing"

	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/packagejson"
	"github.com/microsoft/typescript-go/internal/pnp"
	"github.com/microsoft/typescript-go/internal/vfs"
	"github.com/microsoft/typescript-go/internal/vfs/vfstest"
)

type packageIDResolutionHost struct {
	fs vfs.FS
}

func (h *packageIDResolutionHost) FS() vfs.FS                  { return h.fs }
func (h *packageIDResolutionHost) GetCurrentDirectory() string { return "/repo" }
func (h *packageIDResolutionHost) PnpApi() *pnp.PnpApi         { return nil }

func TestGetPackageIdNormalizesPackageDirectory(t *testing.T) {
	t.Parallel()

	fields, err := packagejson.Parse([]byte(`{"name":"pkg","version":"1.0.0"}`))
	if err != nil {
		t.Fatalf("failed to parse package.json: %v", err)
	}
	host := &packageIDResolutionHost{fs: vfstest.FromMap(map[string]string{}, true)}
	state := &resolutionState{
		resolver: NewResolver(host, &core.CompilerOptions{}, "", ""),
	}

	for _, packageDirectory := range []string{"/repo/pkg", "/repo/pkg/"} {
		packageInfo := &packagejson.InfoCacheEntry{
			PackageDirectory: packageDirectory,
			DirectoryExists:  true,
			Contents:         &packagejson.PackageJson{Fields: fields},
		}
		packageID := state.getPackageId("/repo/pkg/src/index.ts", packageInfo)
		if packageID.Name != "pkg" || packageID.SubModuleName != "src/index.ts" {
			t.Fatalf("unexpected package id for directory %q: %+v", packageDirectory, packageID)
		}
	}
}
