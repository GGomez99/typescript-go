package pnp

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

var (
	isPnpApiInitialized atomic.Uint32
	cachedPnpApi        *PnpApi
	pnpMu               sync.Mutex
	// testPnpCache stores per-goroutine PnP APIs for test isolation
	// Key is goroutine ID (as int)
	testPnpCache sync.Map // map[int]*PnpApi
)

// getGoroutineID returns the current goroutine ID
// It is usually not recommended to work with goroutine IDs, but it is the most non-intrusive way to setup a parallel testing environment for PnP API
func getGoroutineID() int {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	id, _ := strconv.Atoi(idField)
	return id
}

// Sets the PnP API for the given manifest data and manifest directory, used for testing
// This creates a goroutine-specific cache entry that won't interfere with other parallel tests.
func OverridePnpApi(fs PnpApiFS, manifestDataRaw string) *PnpApi {
	if manifestDataRaw == "" {
		return nil
	}

	var pnpApi *PnpApi

	manifestData, err := parseManifestFromData(manifestDataRaw, "/")
	if err != nil {
		pnpApi = nil
	} else if manifestData != nil {
		pnpApi = &PnpApi{
			fs:       fs,
			url:      "/",
			manifest: manifestData,
		}
	}

	// Store in goroutine-specific cache using goroutine ID
	// This allows parallel tests to have isolated PnP configurations
	gid := getGoroutineID()
	testPnpCache.Store(gid, pnpApi)

	return pnpApi
}

// ClearTestPnpCache clears the test-specific PnP API cache for the current goroutine
func ClearTestPnpCache() {
	gid := getGoroutineID()
	testPnpCache.Delete(gid)
}

func InitPnpApi(fs PnpApiFS, filePath string) *PnpApi {
	pnpMu.Lock()
	defer pnpMu.Unlock()
	// Double-check after acquiring lock
	if isPnpApiInitialized.Load() == 1 {
		return cachedPnpApi
	}

	pnpApi := &PnpApi{fs: fs, url: filePath}

	manifestData, err := pnpApi.findClosestPnpManifest()
	if err == nil {
		pnpApi.manifest = manifestData
		cachedPnpApi = pnpApi
	} else {
		// Couldn't load PnP API
		cachedPnpApi = nil
	}

	isPnpApiInitialized.Store(1)
	return cachedPnpApi
}

// GetPnpApi returns the PnP API for the given file path. Will return nil if the PnP API is not available or not initialized
func GetPnpApi(filePath string) *PnpApi {
	// If in a test, check for PnP API overrides
	if testing.Testing() {
		gid := getGoroutineID()
		if api, ok := testPnpCache.Load(gid); ok {
			return api.(*PnpApi)
		}
	}

	// Check if PnP API is already initialized using atomic read (no lock needed)
	if isPnpApiInitialized.Load() == 1 {
		return cachedPnpApi
	}

	return nil
}

// Clears the singleton PnP API cache
func ClearPnpCache() {
	pnpMu.Lock()
	defer pnpMu.Unlock()
	cachedPnpApi = nil
	isPnpApiInitialized.Store(0)
}

func IsInPnpModule(fromFileName string, toFileName string) bool {
	pnpApi := GetPnpApi(fromFileName)
	if pnpApi == nil {
		return false
	}

	fromLocator, _ := pnpApi.FindLocator(fromFileName)
	toLocator, _ := pnpApi.FindLocator(toFileName)
	// The targeted filename is in a pnp module different from the requesting filename
	return fromLocator != nil && toLocator != nil && fromLocator.Name != toLocator.Name
}

func AppendPnpTypeRoots(nmTypes []string, baseDir string, nmFromConfig bool) ([]string, bool) {
	pnpTypes := []string{}
	pnpApi := GetPnpApi(baseDir)
	if pnpApi != nil {
		pnpTypes = pnpApi.GetPnpTypeRoots(baseDir)
	}

	if len(nmTypes) > 0 {
		return append(nmTypes, pnpTypes...), nmFromConfig
	}

	if len(pnpTypes) > 0 {
		return pnpTypes, false
	}

	return nil, false
}
