package pnp

/*
 * Yarn Plug'n'Play (generally referred to as Yarn PnP) is the default installation strategy in modern releases of Yarn.
 * Yarn PnP generates a single Node.js loader file in place of the typical node_modules folder.
 * This loader file, named .pnp.cjs, contains all information about your project's dependency tree, informing your tools as to
 * the location of the packages on the disk and letting them know how to resolve require and import calls.
 *
 * The full specification is available at https://yarnpkg.com/advanced/pnp-spec
 */

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/microsoft/typescript-go/internal/tspath"
)

type PnpApi struct {
	url      string
	manifest *PnpManifestData
}

func (p *PnpApi) RefreshManifest() error {
	var newData *PnpManifestData
	var err error

	if p.manifest == nil {
		newData, err = p.findClosestPnpManifest()
	} else {
		newData, err = parseManifestFromPath(p.manifest.dirPath)
	}

	if err != nil {
		return err
	}

	p.manifest = newData
	return nil
}

func (p *PnpApi) ResolveToUnqualified(specifier string, parentPath string) (string, error) {
	if p.manifest == nil {
		panic(fmt.Errorf("ResolveToUnqualified called with no PnP manifest available"))
	}

	ident, modulePath, err := p.ParseBareIdentifier(specifier)
	if err != nil {
		// Skipping resolution
		return "", nil
	}

	parentLocator, err := p.FindLocator(parentPath)
	if err != nil || parentLocator == nil {
		// Skipping resolution
		return "", nil
	}

	parentPkg := p.GetPackage(parentLocator)

	var referenceOrAlias *PackageDependency
	for _, dep := range parentPkg.PackageDependencies {
		if dep.Ident == ident {
			referenceOrAlias = &dep
			break
		}
	}

	// If not found, try fallback if enabled
	if referenceOrAlias == nil {
		if p.manifest.enableTopLevelFallback {
			excluded := false
			if exclusion, ok := p.manifest.fallbackExclusionMap[parentLocator.Name]; ok {
				for _, entry := range exclusion.Entries {
					if entry == parentLocator.Reference {
						excluded = true
						break
					}
				}
			}
			if !excluded {
				fallback := p.ResolveViaFallback(ident)
				if fallback != nil {
					referenceOrAlias = fallback
				}
			}
		}
	}

	// undeclared dependency
	if referenceOrAlias == nil {
		if parentLocator.Name == "" {
			return "", fmt.Errorf("Your application tried to access %s, but it isn't declared in your dependencies; this makes the require call ambiguous and unsound.\n\nRequired package: %s\nRequired by: %s", ident, ident, parentPath)
		}
		return "", fmt.Errorf("%s tried to access %s, but it isn't declared in your dependencies; this makes the require call ambiguous and unsound.\n\nRequired package: %s\nRequired by: %s", parentLocator.Name, ident, ident, parentPath)
	}

	// unfulfilled peer dependency
	if !referenceOrAlias.IsAlias() && referenceOrAlias.Reference == "" {
		if parentLocator.Name == "" {
			return "", fmt.Errorf("Your application tried to access %s (a peer dependency); this isn't allowed as there is no ancestor to satisfy the requirement. Use a devDependency if needed.\n\nRequired package: %s\nRequired by: %s", ident, ident, parentPath)
		}
		return "", fmt.Errorf("%s tried to access %s (a peer dependency) but it isn't provided by its ancestors/your application; this makes the require call ambiguous and unsound.\n\nRequired package: %s\nRequired by: %s", parentLocator.Name, ident, ident, parentPath)
	}

	var dependencyPkg *PackageInfo
	if referenceOrAlias.IsAlias() {
		dependencyPkg = p.GetPackage(&Locator{Name: referenceOrAlias.AliasName, Reference: referenceOrAlias.Reference})
	} else {
		dependencyPkg = p.GetPackage(&Locator{Name: referenceOrAlias.Ident, Reference: referenceOrAlias.Reference})
	}

	return filepath.Join(p.manifest.dirPath, dependencyPkg.PackageLocation, modulePath), nil
}

func (p *PnpApi) findClosestPnpManifest() (*PnpManifestData, error) {
	directoryPath := p.url

	for {
		pnpPath := path.Join(directoryPath, ".pnp.cjs")
		if _, err := os.Stat(pnpPath); err == nil {
			return parseManifestFromPath(directoryPath)
		}

		directoryPath = path.Dir(directoryPath)
		if directoryPath == "/" {
			return nil, fmt.Errorf("no PnP manifest found")
		}
	}
}

func (p *PnpApi) GetPackage(locator *Locator) *PackageInfo {
	packageRegistryMap := p.manifest.packageRegistryMap
	packageInfo, ok := packageRegistryMap[locator.Name][locator.Reference]
	if !ok {
		panic(fmt.Sprintf("%s should have an entry in the package registry", locator.Name))
	}

	return packageInfo
}

func (p *PnpApi) FindLocator(parentPath string) (*Locator, error) {
	relativePath, err := filepath.Rel(p.manifest.dirPath, parentPath)
	if err != nil {
		return nil, err
	}

	if p.manifest.ignorePatternData != nil {
		match, err := p.manifest.ignorePatternData.MatchString(relativePath)
		if err != nil {
			return nil, err
		}

		if match {
			return nil, nil
		}
	}

	var relativePathWithDot string
	if strings.HasPrefix(relativePath, "../") {
		relativePathWithDot = relativePath
	} else {
		relativePathWithDot = "./" + relativePath
	}

	pathSegments := strings.Split(relativePathWithDot, "/")
	currentTrie := p.manifest.packageRegistryTrie

	// Go down the trie, looking for the latest defined packageInfo that matches the path
	for _, segment := range pathSegments {
		if currentTrie.childrenPathSegments[segment] == nil {
			break
		}

		currentTrie = currentTrie.childrenPathSegments[segment]
	}

	if currentTrie.packageData == nil {
		return nil, fmt.Errorf("no package found for path %s", relativePath)
	}

	return &Locator{Name: currentTrie.packageData.ident, Reference: currentTrie.packageData.reference}, nil
}

func (p *PnpApi) ResolveViaFallback(name string) *PackageDependency {
	topLevelPkg := p.GetPackage(&Locator{Name: "", Reference: ""})

	if topLevelPkg != nil {
		for _, dep := range topLevelPkg.PackageDependencies {
			if dep.Ident == name {
				return &dep
			}
		}
	}

	for _, dep := range p.manifest.fallbackPool {
		if dep[0] == name {
			return &PackageDependency{
				Ident:     dep[0],
				Reference: dep[1],
				AliasName: "",
			}
		}
	}

	return nil
}

func (p *PnpApi) ParseBareIdentifier(specifier string) (ident string, modulePath string, err error) {
	if len(specifier) == 0 {
		return "", "", fmt.Errorf("Empty specifier: %s", specifier)
	}

	firstSlash := strings.Index(specifier, "/")

	if specifier[0] == '@' {
		if firstSlash == -1 {
			return "", "", fmt.Errorf("Invalid specifier: %s", specifier)
		}

		secondSlash := strings.Index(specifier[firstSlash+1:], "/")

		if secondSlash == -1 {
			ident = specifier
		} else {
			ident = specifier[:firstSlash+1+secondSlash]
		}
	} else {
		firstSlash := strings.Index(specifier, "/")

		if firstSlash == -1 {
			ident = specifier
		} else {
			ident = specifier[:firstSlash]
		}
	}

	modulePath = specifier[len(ident):]

	return ident, modulePath, nil
}

func (p *PnpApi) GetPnpTypeRoots(currentDirectory string) []string {
	if p.manifest == nil {
		return []string{}
	}

	currentDirectory = tspath.NormalizePath(currentDirectory)

	currentPackage, err := p.FindLocator(currentDirectory)
	if err != nil {
		return []string{}
	}

	if currentPackage == nil {
		return []string{}
	}

	packageDependencies := p.GetPackage(currentPackage).PackageDependencies

	typeRoots := []string{}
	for _, dep := range packageDependencies {
		if strings.HasPrefix(dep.Ident, "@types/") && dep.Reference != "" {
			packageInfo := p.GetPackage(&Locator{Name: dep.Ident, Reference: dep.Reference})
			typeRoots = append(typeRoots, path.Dir(path.Join(p.manifest.dirPath, packageInfo.PackageLocation)))
		}
	}

	return typeRoots
}
