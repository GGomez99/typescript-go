package pnp

import (
	"sync"
	"sync/atomic"
)

var (
	isPnpApiInitialized atomic.Uint32
	cachedPnpApi        *PnpApi
	pnpMu               sync.Mutex
)

// Clears the singleton PnP API cache
func ClearPnpCache() {
	pnpMu.Lock()
	defer pnpMu.Unlock()
	cachedPnpApi = nil
	isPnpApiInitialized.Store(0)
}

// GetPnpApi returns the PnP API for the given file path. Will return nil if the PnP API is not available.
func GetPnpApi(filePath string) *PnpApi {
	// Check if PnP API is already initialized using atomic read (no lock needed)
	if isPnpApiInitialized.Load() == 1 {
		return cachedPnpApi
	}

	pnpMu.Lock()
	defer pnpMu.Unlock()
	// Double-check after acquiring lock
	if isPnpApiInitialized.Load() == 1 {
		return cachedPnpApi
	}

	pnpApi := &PnpApi{url: filePath}

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
