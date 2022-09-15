package decompiler

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const SYSTEM_PACKAGE = "__system"

type PackageInfo struct {
	name         string
	functions    []*FunctionDeclaration
	dependencies map[string]bool
	handles      map[string]bool
	enums        map[string]bool
}

type ManualDependencyActions struct {
	add    []string
	remove []string
}

// HACK: We can't completely rely on the 'uses' statements in the headers because they sometimes leave out
// some dependencies. I will hard code the missing ones here for now so we don't have to modify the headers.
var MANUAL_DEPENDENCIES = map[string]ManualDependencyActions{
	"iDockport":    {add: []string{"iSim"}},
	"iLoadout":     {add: []string{"GUI"}},
	"Sim":          {remove: []string{"Subsim"}},
	"Subsim":       {add: []string{"Sim"}},
	"Object":       {add: []string{"List"}},
	"iScore":       {add: []string{"iShip"}},
	"iCargoScript": {add: []string{"iHabitat"}},
}

var PACKAGES = map[string]*PackageInfo{}

// func (pkg *PackageInfo) dependsOnInternal(base string, visited map[string]bool) bool {

// 	visited[fmt.Sprintf("%s->%s", pkg.name, base)] = true

// 	for dependency := range pkg.dependencies {
// 		if dependency == base {
// 			return true
// 		}

// 		if depPkg, ok := PACKAGES[strings.ToLower(dependency)]; ok {
// 			visitStr := fmt.Sprintf("%s->%s", depPkg.name, base)
// 			_, already := visited[visitStr]
// 			if already {
// 				continue
// 			}
// 			if depPkg.dependsOnInternal(base, visited) {
// 				return true
// 			}
// 		}
// 	}

// 	return false
// }

func (pkg *PackageInfo) DependsOn(base string) bool {
	_, exists := pkg.dependencies[base]
	return exists
	// visited := map[string]bool{}
	// return pkg.dependsOnInternal(base, visited)
}

func (pkg *PackageInfo) DetectDepdencies() {
	pkg.dependencies = map[string]bool{}

	// Check the package's handle definitions
	for handle := range pkg.handles {
		hnd := HANDLE_MAP[handle]
		if base, ok := HANDLE_MAP[hnd.baseType]; ok {
			if base.sourcePackage != SYSTEM_PACKAGE {
				pkg.dependencies[base.sourcePackage] = true
			}
		}
	}

	// Check the package's functions
	for _, fnc := range pkg.functions {
		// Check the return type
		if handleInfo, ok := HANDLE_MAP[fnc.GetReturnType()]; ok {
			if handleInfo.sourcePackage != pkg.name && handleInfo.sourcePackage != SYSTEM_PACKAGE {
				pkg.dependencies[handleInfo.sourcePackage] = true
			}
		}

		// Check the parameters
		if fnc.parameters != nil {
			for _, p := range *fnc.parameters {
				if handleInfo, ok := HANDLE_MAP[p.typeName]; ok {
					if handleInfo.sourcePackage != pkg.name && handleInfo.sourcePackage != SYSTEM_PACKAGE {
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

func IsValidIdentifier(name string) bool {
	if len(name) == 0 {
		return false
	}

	for ii := range name {
		r := rune(name[ii])
		// We can't start with a number
		if ii == 0 && unicode.IsNumber(r) {
			return false
		}

		// We can only have letters, numbers, and underscores
		if !unicode.IsNumber(r) &&
			!unicode.IsLetter(r) &&
			r != '_' {
			return false
		}
	}
	return true
}

func ConvertToIdentifier(name string) string {
	if strings.HasPrefix(name, "ini:/") {
		split := strings.Split(name, "/")
		if len(split) > 0 {
			return split[len(split)-1]
		} else {
			return ""
		}
	}
	result := []byte{}
	idx := 0
	for ii := range name {
		r := rune(name[ii])

		if unicode.IsSpace(r) || r == '-' || r == '\'' {
			continue
		}
		// We can't start with a number
		if idx == 0 && unicode.IsNumber(r) {
			result = append(result, '_')
			idx++
			continue
		}

		// We can only have letters, numbers, and underscores
		if !unicode.IsNumber(r) &&
			!unicode.IsLetter(r) &&
			r != '_' {
			result = append(result, '_')
			idx++
			continue
		}

		if idx == 0 {
			result = append(result, byte(unicode.ToLower(r)))
		} else {
			result = append(result, name[ii])
		}
		idx++
	}
	return string(result)
}

func parsePackageHandles(contents []byte, pkg *PackageInfo) {
	// Find all handle declarations
	r, _ := regexp.Compile("(handle[^:]*:[^;]*;)")

	all := r.FindAll(contents, -1)

	for ii := range all {
		handle := string(all[ii][len("handle") : len(all[ii])-1])
		parts := strings.Split(handle, ":")
		typeName := strings.TrimSpace(parts[0])
		baseType := strings.TrimSpace(parts[1])

		if !IsValidIdentifier(typeName) || !IsValidIdentifier(baseType) {
			fmt.Printf("ERROR: Failed to parse package %s handle definition '%s', invalid identifier.\n", pkg.name, all[ii])
			continue
		}

		HANDLE_MAP[typeName] = HandleTypeInfo{
			baseType:      baseType,
			sourcePackage: pkg.name,
		}
		pkg.handles[typeName] = true
	}
}

func parsePackageDependencies(contents []byte, pkg *PackageInfo) {
	// Find all handle declarations
	r, _ := regexp.Compile(`([\s]uses[^;]*;)`)

	all := r.FindAll(contents, -1)

	// If we can't find any "uses" statements, we need to detect dependencies on our own
	if len(all) == 0 {
		pkg.dependencies = nil
		return
	}

	for ii := range all {
		depList := string(all[ii])
		depList = strings.TrimPrefix(depList[1:], "uses")
		depList = strings.TrimSuffix(depList, ";")
		deps := strings.Split(string(depList), ",")
		for _, dep := range deps {
			dep = strings.TrimSpace(dep)
			if !IsValidIdentifier(dep) {
				fmt.Printf("ERROR: Failed to parse package %s dependency list '%s', invalid identifier %s.\n", pkg.name, all[ii], dep)
				continue
			}
			pkg.dependencies[strings.TrimSpace(dep)] = true
		}
	}
}

func parsePackageEnums(contents []byte, pkg *PackageInfo) {
	// Find all handle declarations
	r, _ := regexp.Compile("(enum[^}]*})")

	all := r.FindAll(contents, -1)

	for ii := range all {
		hasError := false
		enumData := EnumTypeInfo{
			valueToName: map[uint32]string{},
			nameToValue: map[string]uint32{},
		}
		enumDef := string(all[ii])
		enumDef = strings.TrimPrefix(enumDef, "enum")
		enumDef = strings.TrimSuffix(enumDef, "}")

		// Get the name
		parts := strings.Split(enumDef, "{")

		if len(parts) != 2 || len(strings.TrimSpace(parts[0])) == 0 {
			fmt.Printf("WARN: Enum name missing for enum in package %s header.\n", pkg.name)
			continue
		}

		enumName := strings.TrimSpace(parts[0])

		if !IsValidIdentifier(enumName) {
			fmt.Printf("WARN: Invalid enum name %s in package %s header.\n", enumName, pkg.name)
			continue
		}

		// Figure out the members
		members := strings.Split(parts[1], ",")
		var nextValue uint32 = 0

		for _, member := range members {
			var value uint32
			var name string
			parts := strings.Split(member, "=")
			if len(parts) == 2 {
				var err error
				name = strings.TrimSpace(parts[0])
				valueStr := strings.TrimSpace(parts[1])
				if strings.Contains(valueStr, "|") {
					names := strings.Split(valueStr, "|")
					value = 0
					for _, name := range names {
						var subValue uint32
						var ok bool
						name = strings.TrimSpace(name)
						if subValue, ok = enumData.nameToValue[name]; !ok {
							err = fmt.Errorf("failed to find referenced member value for %s", name)
							break
						}
						value = uint32(value) | subValue
					}
				} else {
					base := 10
					if strings.HasPrefix(valueStr, "0x") {
						base = 16
						// Skip the prefix
						valueStr = valueStr[2:]
					}
					var v int64
					v, err = strconv.ParseInt(valueStr, base, 64)
					value = uint32(v)
				}
				if err != nil {
					fmt.Printf("WARN: Failed to parse value for enum %s member %s: %s, %v\n.", enumName, name, valueStr, err)
					hasError = true
					break
				}
			} else if len(parts) == 1 {
				value = nextValue
				name = strings.TrimSpace(member)
			} else {
				fmt.Printf("ERROR: Failed to process member for enum %s.\n", enumName)
				hasError = true
				break
			}

			if !IsValidIdentifier(name) {
				fmt.Printf("WARN: Invalid identifier for enum %s member %s.\n.", enumName, name)
				hasError = true
				break
			}

			enumData.nameToValue[name] = value
			enumData.valueToName[value] = name
			nextValue = value + 1
		}

		if !hasError {
			// Save off this enum
			ENUM_MAP[enumName] = enumData
			pkg.enums[enumName] = true
		}
	}
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
		enums:        map[string]bool{},
	}
	PACKAGES[strings.ToLower(packageName)] = &packageInfo

	contents, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
		return
	}

	contents = removeComments(contents)

	parsePackageHandles(contents, &packageInfo)
	parsePackageDependencies(contents, &packageInfo)
	parsePackageEnums(contents, &packageInfo)

	fileScanner := bufio.NewScanner(bytes.NewReader(contents))
	fileScanner.Split(scanPrototypes)

	for fileScanner.Scan() {
		result := fileScanner.Text()

		prototype := strings.ReplaceAll(result, "\t", " ")
		prototype = strings.ReplaceAll(prototype, "\r", " ")
		prototype = strings.ReplaceAll(prototype, "\n", " ")

		declaration := AddFunctionDeclarationFromPrototype(prototype)
		if declaration != nil {
			if len(declaration.pkg) > 0 {
				packageInfo.name = declaration.pkg
			}
			packageInfo.functions = append(packageInfo.functions, declaration)
		}
	}
}

func LoadDeclarationsFromHeaders(includeDir string) {
	filepath.WalkDir(includeDir, parseEntry)
}

func DetectPackageDependencies() {
	// Look through every function in every package to see if they have parameters or return types from other packages
	for _, pkg := range PACKAGES {
		if pkg.dependencies == nil {
			pkg.DetectDepdencies()
		}

		depActions := MANUAL_DEPENDENCIES[pkg.name]
		// Add in the manual hack dependencies
		for _, dep := range depActions.add {
			pkg.dependencies[dep] = true
		}

		// Remove the manual hack dependencies
		for _, dep := range depActions.remove {
			delete(pkg.dependencies, dep)
		}
	}
}
