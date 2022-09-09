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

var PACKAGES = map[string]string{}

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
	PACKAGES[strings.ToLower(packageName)] = packageName

	contents, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
		return
	}

	contents = removeComments(contents)

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

		AddFunctionDeclarationFromPrototype(prototype)
	}
}

func LoadFunctionDeclarationsFromHeaders(includeDir string) {
	filepath.WalkDir(includeDir, parseEntry)
}
