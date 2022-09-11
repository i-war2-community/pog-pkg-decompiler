package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type PackageInfo struct {
	name         string
	functions    []*FunctionDeclaration
	dependencies map[string]bool
	handles      map[string]bool
}

var PACKAGES = map[string]*PackageInfo{}

func (pkg *PackageInfo) dependsOnInternal(base string, visited map[string]bool) bool {

	visited[fmt.Sprintf("%s->%s", pkg.name, base)] = true

	for dependency := range pkg.dependencies {
		if dependency == base {
			return true
		}

		if depPkg, ok := PACKAGES[strings.ToLower(dependency)]; ok {
			visitStr := fmt.Sprintf("%s->%s", depPkg.name, base)
			_, already := visited[visitStr]
			if already {
				continue
			}
			if depPkg.dependsOnInternal(base, visited) {
				return true
			}
		}
	}

	return false
}

func (pkg *PackageInfo) DependsOn(base string) bool {
	visited := map[string]bool{}
	return pkg.dependsOnInternal(base, visited)
}

func (pkg *PackageInfo) DetectDepdencies() {
	// Check the package's handle definitions
	for handle := range pkg.handles {
		hnd := HANDLE_MAP[handle]
		if base, ok := HANDLE_MAP[hnd.baseType]; ok {
			pkg.dependencies[base.sourcePackage] = true
		}
	}

	// Check the package's functions
	for _, fnc := range pkg.functions {
		// Check the return type
		if handleInfo, ok := HANDLE_MAP[fnc.returnTypeName]; ok {
			if handleInfo.sourcePackage != pkg.name {
				pkg.dependencies[handleInfo.sourcePackage] = true
			}
		}

		// Check the parameters
		if fnc.parameters != nil {
			for _, p := range *fnc.parameters {
				if handleInfo, ok := HANDLE_MAP[p.typeName]; ok {
					if handleInfo.sourcePackage != pkg.name {
						pkg.dependencies[handleInfo.sourcePackage] = true
					}
				}
			}
		}
	}
}

func parseEntry(path string, d fs.DirEntry, err error) error {
	if strings.ToLower(filepath.Ext(path)) == ".h" {
		parseInclude(path)
	}
	return nil
}

func scanPrototypes(data []byte, atEOF bool) (advance int, token []byte, err error) {
	start := bytes.Index(data, []byte(PROTOTYPE_PREFIX))

	// If we can't find prototype we should advance some
	if start < 0 {
		advance = len(data) - len(PROTOTYPE_PREFIX)
		if advance < 0 {
			advance = 0
		}
		return advance, nil, nil
	}

	end := bytes.Index(data[start:], []byte(";"))
	if end < 0 {
		if atEOF {
			return 0, nil, fmt.Errorf("prototype missing ;")
		}
		return start, nil, nil
	}

	end += start

	prototype := data[start:end]

	return end, prototype, nil
}

func removeComments(contents []byte) []byte {

	// Remove any line comments
	r, _ := regexp.Compile("(//.*\n)")
	contents = r.ReplaceAll(contents, []byte{})

	// Remove any block comments
	r, _ = regexp.Compile(`(/\*.*\*/)`)
	contents = r.ReplaceAll(contents, []byte{})

	return contents
}

func parseInclude(path string) {

	// Save off the package name with the proper upper and lower cases based on the filenames (for now, so far this seems to match)
	packageName := filepath.Base(path)
	packageName = strings.TrimSuffix(packageName, filepath.Ext(packageName))
	packageInfo := PackageInfo{
		name:         packageName,
		functions:    []*FunctionDeclaration{},
		dependencies: map[string]bool{},
		handles:      map[string]bool{},
	}
	PACKAGES[strings.ToLower(packageName)] = &packageInfo

	contents, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
		return
	}

	contents = removeComments(contents)

	r, _ := regexp.Compile("(handle.*:.*;)")

	all := r.FindAll(contents, -1)

	for ii := range all {
		handle := string(all[ii][len("handle") : len(all[ii])-1])
		parts := strings.Split(handle, ":")
		typeName := strings.TrimSpace(parts[0])
		HANDLE_MAP[typeName] = HandleTypeInfo{
			baseType:      strings.TrimSpace(parts[1]),
			sourcePackage: packageName,
		}
		packageInfo.handles[typeName] = true
	}

	fileScanner := bufio.NewScanner(bytes.NewReader(contents))
	fileScanner.Split(scanPrototypes)

	for fileScanner.Scan() {
		result := fileScanner.Text()

		prototype := string(r.ReplaceAll([]byte(result), []byte{}))

		prototype = strings.ReplaceAll(prototype, "\t", "")
		prototype = strings.ReplaceAll(prototype, "\r", "")
		prototype = strings.ReplaceAll(prototype, "\n", "")

		declaration := AddFunctionDeclarationFromPrototype(prototype)
		if declaration != nil {
			if len(declaration.pkg) > 0 {
				packageInfo.name = declaration.pkg
			}
			packageInfo.functions = append(packageInfo.functions, declaration)
		}
	}
}

func LoadFunctionDeclarationsFromHeaders(includeDir string) {
	filepath.WalkDir(includeDir, parseEntry)
}

func DetectPackageDependencies() {
	// Look through every function in every package to see if they have parameters or return types from other packages
	for _, pkg := range PACKAGES {
		pkg.DetectDepdencies()
	}
}
