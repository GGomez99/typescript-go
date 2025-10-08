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
