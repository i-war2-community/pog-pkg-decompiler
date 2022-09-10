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
}

var PACKAGES = map[string]*PackageInfo{}

func (pkg *PackageInfo) DetectDepdencies() {
	for _, fnc := range pkg.functions {
		// Check the return type
		if handleInfo, ok := HANDLE_MAP[fnc.returnTypeName]; ok {
			pkg.dependencies[handleInfo.sourcePackage] = true
		}

		// Check the parameters
		if fnc.parameters != nil {
			for _, p := range *fnc.parameters {
				if handleInfo, ok := HANDLE_MAP[p.typeName]; ok {
					pkg.dependencies[handleInfo.sourcePackage] = true
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
		HANDLE_MAP[strings.TrimSpace(parts[0])] = HandleTypeInfo{
			baseType:      strings.TrimSpace(parts[1]),
			sourcePackage: packageName,
		}
	}

	fileScanner := bufio.NewScanner(bytes.NewReader(contents))
	fileScanner.Split(scanPrototypes)

	for fileScanner.Scan() {
		result := fileScanner.Text()

		// Now we need to remove any line comments
		r, _ := regexp.Compile("(//.*\n)")

		prototype := string(r.ReplaceAll([]byte(result), []byte{}))

		prototype = strings.ReplaceAll(prototype, "\t", "")
		prototype = strings.ReplaceAll(prototype, "\r", "")
		prototype = strings.ReplaceAll(prototype, "\n", "")

		declaration := AddFunctionDeclarationFromPrototype(prototype)
		if declaration != nil {
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
