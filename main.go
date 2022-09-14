package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Command line options
var INCLUDES_DIR string
var OUTPUT_FILE string
var OUTPUT_ASSEMBLY bool
var ASSEMBLY_ONLY bool
var ASSEMBLY_OFFSET_PREFIX bool
var DEBUG_LOGGING bool

var EXPORTING_PACKAGE string
var IMPORTING_PACKAGE string

var FUNC_EXPORTS []*FunctionDeclaration = []*FunctionDeclaration{}
var PACKAGE_IMPORTS []string = []string{}

var DECOMPILED_FUNCS []*FunctionDefinition = []*FunctionDefinition{}

var FUNC_DEFINITION_MAP map[uint32]*FunctionDeclaration = map[uint32]*FunctionDeclaration{}
var FUNC_IMPORT_MAP map[uint32]*FunctionDeclaration = map[uint32]*FunctionDeclaration{}

var STRING_TABLE = []string{}
var OPERATIONS = []Operation{}

type SectionHeader struct {
	identifier string
	length     uint32
}

func sortPackageImports(imports []string) []string {
	results := []string{}

	for _, imp := range imports {
		inserted := false
		for idx, result := range results {
			resultPkg := PACKAGES[strings.ToLower(result)]
			if resultPkg != nil && resultPkg.DependsOn(imp) {
				results = append(results[:idx+1], results[idx:]...)
				results[idx] = imp
				inserted = true
				break
			}
		}
		if !inserted {
			results = append(results, imp)
		}

	}

	return results
}

func renderPackageImports(writer CodeWriter) {
	importCount := len(PACKAGE_IMPORTS)
	if importCount == 0 {
		return
	}

	// Add any missing dependency imports
	imports := []string{}
	import_map := map[string]bool{}

	// First get our existing imports and map them
	for _, pkgName := range PACKAGE_IMPORTS {
		imports = append(imports, pkgName)
		import_map[pkgName] = true
	}

	// This is needed to call the logging function in script
	if DEBUG_LOGGING {
		if _, ok := import_map["Debug"]; !ok {
			imports = append(imports, "Debug")
			import_map["Debug"] = true
		}
	}

	// Sort the initial set of imports so the output will be deterministic (maps won't iterate in the same order each time)
	sort.Strings(imports)

	// Now check for any missing dependencies
	for ii := 0; ii < len(imports); ii++ {
		pkgName := imports[ii]
		if pkg, ok := PACKAGES[strings.ToLower(pkgName)]; ok {
			for dep := range pkg.dependencies {
				_, exists := import_map[dep]
				if !exists {
					imports = append(imports, dep)
					import_map[dep] = true
				}
			}
		}
	}

	imports = sortPackageImports(imports)

	importCount = len(imports)

	writer.Append("uses ")
	for ii := 0; ii < importCount; ii++ {
		if ii > 0 {
			writer.Append("     ")
		}
		writer.Append(imports[ii])
		if ii < importCount-1 {
			writer.Append(",\n")
		}
	}
	writer.Append(";\n\n")
}

func renderFunctionExports(writer CodeWriter) {
	exportCount := len(FUNC_EXPORTS)
	if exportCount == 0 {
		return
	}
	writer.Append("provides ")
	for ii := 0; ii < exportCount; ii++ {
		if ii > 0 {
			writer.Append("         ")
		}
		writer.Append(FUNC_EXPORTS[ii].name)
		if ii < exportCount-1 {
			writer.Append(",\n")
		}
	}
	writer.Append(";\n\n")
}

func renderEnums(writer CodeWriter) {
	pkg, ok := PACKAGES[strings.ToLower(EXPORTING_PACKAGE)]
	if ok {
		for enumName := range pkg.enums {
			writer.Appendf("enum %s\n", enumName)
			writer.Append("{\n")
			writer.PushIndent()
			keys := []uint32{}
			for k := range ENUM_MAP[enumName].valueToName {
				keys = append(keys, k)
			}

			// Sort the array
			sort.Slice(keys, func(i, j int) bool {
				return keys[i] < keys[j]
			})

			for ii, k := range keys {
				writer.Appendf("%s = 0x%08X", ENUM_MAP[enumName].valueToName[k], k)
				if ii < len(keys)-1 {
					writer.Append(",\n")
				} else {
					writer.Append("\n")
				}
			}
			writer.PopIndent()
			writer.Append("};\n\n")
		}
	}
}

func readString(file *os.File) (string, error) {
	var result []byte
	var err error = nil

	buffer := make([]byte, 1)
	for err == nil {
		_, err = file.Read(buffer)
		if err != nil || buffer[0] == 0 {
			break
		}
		result = append(result, buffer[0])
	}

	if err != nil {
		return string(result), err
	}

	return string(result), nil
}

func readUInt32BigEndian(file *os.File) (uint32, error) {
	buffer := make([]byte, 4)
	n, err := file.Read(buffer)
	if err != nil {
		return 0, err
	}
	if n != 4 {
		return 0, fmt.Errorf("EOF")
	}

	return binary.BigEndian.Uint32(buffer), nil
}

func readSectionHeader(file *os.File) (*SectionHeader, error) {
	result := new(SectionHeader)

	// Read the section identifier
	buffer := make([]byte, 4)
	n, err := file.Read(buffer)
	if err != nil {
		return nil, err
	}
	if n != 4 {
		return nil, fmt.Errorf("header not long enough")
	}

	result.identifier = string(buffer)

	// Read the section length
	result.length, err = readUInt32BigEndian(file)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func readSections(file *os.File, maximumLength uint32) error {

	var length uint32 = 0

	for length < maximumLength {
		section, err := readSectionHeader(file)
		if err != nil {
			fmt.Printf("Error: Failed to read section: %v", err)
			return err
		}
		length += 8
		//fmt.Printf("%s ", section.identifier)
		// Get the original offset
		start, err := file.Seek(0, 1)
		if err != nil {
			fmt.Printf("Error: Failed to get file offset: %v", err)
			return err
		}

		err = readSection(file, section)
		if err != nil {
			fmt.Printf("Error: Failed to read section: %v", err)
			return err
		}

		//fmt.Printf("\n")

		seek := section.length
		// If the length is odd, add one so we will be 2 byte aligned
		seek += seek % 2
		file.Seek(start+int64(seek), 0)
		length += seek
	}
	return nil
}

func readSection(file *os.File, section *SectionHeader) error {
	var err error = nil

	switch section.identifier {
	case "PIMP":
		name, err := readString(file)
		if err != nil {
			return err
		}
		//fmt.Printf("%s", name)
		if name != SYSTEM_PACKAGE {
			_, ok := PACKAGES[name]
			if !ok {
				fmt.Printf("ERROR: Importing package '%s' not found in includes!\n", name)
				//os.Exit(1)
			} else {
				// Get the package name with the correct upper and lower case letters
				name = PACKAGES[name].name
			}

			PACKAGE_IMPORTS = append(PACKAGE_IMPORTS, name)
		}
		IMPORTING_PACKAGE = name
	case "FIMP":
		name, err := readString(file)
		if err != nil {
			return err
		}
		//fmt.Printf("%s", name)
		readFunctionImportSection(file, name)

	case "PKHD":
		name, err := readString(file)
		if err != nil {
			return err
		}
		//fmt.Printf("%s", name)
		lookup, ok := PACKAGES[name]
		if !ok {
			fmt.Printf("WARN: Exporting package '%s' not found in includes. Package name might be output with incorrect case!\n", name)
			//os.Exit(1)
		} else {
			name = lookup.name
		}
		EXPORTING_PACKAGE = name

	case "FEXP":
		name, err := readString(file)
		if err != nil {
			return err
		}
		funcOffset, err := readUInt32BigEndian(file)
		if err != nil {
			return err
		}

		// Create a new declaration for this function
		declaration := AddFunctionDeclaration(EXPORTING_PACKAGE, name)

		// Add it to the list of exports
		FUNC_EXPORTS = append(FUNC_EXPORTS, declaration)

		// Add it to the definition map
		FUNC_DEFINITION_MAP[funcOffset] = declaration

	case "STAB":
		// Read in the string table
		strCount, err := readUInt32BigEndian(file)
		if err != nil {
			return err
		}
		var idx uint32
		for idx = 0; idx < strCount; idx++ {
			str, err := readString(file)
			if err != nil {
				return err
			}
			STRING_TABLE = append(STRING_TABLE, str)
		}

	case "CODE":
		err = readCodeSection(file)
	}

	return err
}

func readFunctionImportSection(file *os.File, funcName string) error {
	refCount, err := readUInt32BigEndian(file)
	if err != nil {
		return err
	}

	var idx uint32
	for idx = 0; idx < refCount; idx++ {
		offset, err := readUInt32BigEndian(file)
		if err != nil {
			return err
		}
		//fmt.Printf(" 0x%08X", offset)

		declaration := AddFunctionDeclaration(IMPORTING_PACKAGE, funcName)
		FUNC_IMPORT_MAP[offset] = declaration
	}

	return nil
}

func readCodeSection(file *os.File) error {

	buffer := make([]byte, 4)
	n, err := file.Read(buffer)
	if err != nil {
		return err
	}
	if n != 4 {
		return fmt.Errorf("code not long enough")
	}
	codeLength := binary.BigEndian.Uint32(buffer)

	initialOffset, _ := file.Seek(0, 1)

	buffer = make([]byte, codeLength)
	_, err = file.Read(buffer)
	if err != nil {
		return err
	}

	var offset uint32

	for offset = 0; offset < codeLength; {

		opcode := buffer[offset]

		opInfo, ok := OP_MAP[opcode]
		if !ok {
			return fmt.Errorf("unknown opcode 0x%02X at position 0x%08X", opcode, offset+uint32(initialOffset))
		}

		operation := new(Operation)
		operation.opcode = opcode
		operation.offset = offset

		offset++

		if opInfo.parser != nil {
			data := buffer[offset : offset+uint32(opInfo.dataSize)]
			operation.data = opInfo.parser(data, offset-1)
		}

		offset += uint32(opInfo.dataSize)

		OPERATIONS = append(OPERATIONS, *operation)
	}

	for idx := 0; idx < len(OPERATIONS); idx++ {
		if idx == 0 || OPERATIONS[idx-1].opcode == OP_FUNCTION_END {
			operation := OPERATIONS[idx]
			declaration := FUNC_DEFINITION_MAP[operation.offset]

			// See if we have an unreferenced function here
			if declaration == nil {
				declaration = NewLocalFunctionAtOffset(uint32(idx))
			}

			var def *FunctionDefinition
			idx, def = DecompileFunction(declaration, idx, initialOffset)
			DECOMPILED_FUNCS = append(DECOMPILED_FUNCS, def)
		}
	}

	return nil
}

func resolveAllTypes() {
	for {
		// Resolve the types for each function
		resolveCount := 0

		// First reset all the possible types
		for _, fnc := range DECOMPILED_FUNCS {
			fnc.ResetPossibleTypes()
		}

		// Call the type resolution done on the statements
		for _, fnc := range DECOMPILED_FUNCS {
			resolveCount += fnc.ResolveBodyTypes()
		}

		// Call the type resolution done on the function parameters and return types
		for _, fnc := range DECOMPILED_FUNCS {
			resolveCount += fnc.ResolveDeclarationTypes()
		}
		if resolveCount == 0 {
			break
		}
	}
}

func checkAllCode() {
	// Check the code of each function
	for _, fnc := range DECOMPILED_FUNCS {
		fnc.CheckCode()
	}
}

func resolveAllNames() {
	// First reset all the possible types
	for _, fnc := range DECOMPILED_FUNCS {
		fnc.ResolveAllNames()
	}
}

func createWriter() (CodeWriter, error) {
	fmt.Printf("Writing pog: %s\n", OUTPUT_FILE)

	outputFile, err := os.OpenFile(OUTPUT_FILE, os.O_WRONLY|os.O_CREATE|os.O_SYNC|os.O_TRUNC, 0644)

	if err != nil {
		return nil, err
	}
	return NewCodeWriter(outputFile), nil
}

func main() {
	flag.StringVar(&INCLUDES_DIR, "includes", "", "The includes directory with package headers.")
	flag.StringVar(&OUTPUT_FILE, "output", "", "The file path to which the pog file will be written.")
	flag.BoolVar(&OUTPUT_ASSEMBLY, "assembly", false, "Have the decompiler output the 'assembly' for each function as comments above the function.")
	flag.BoolVar(&ASSEMBLY_ONLY, "assembly-only", false, "Have the decompiler output only the assembly for the package.")
	flag.BoolVar(&ASSEMBLY_OFFSET_PREFIX, "assembly-offset-prefix", true, "Prefix each line of assembly with its binary address.")
	flag.BoolVar(&DEBUG_LOGGING, "debug", false, "Output code that logs debug info at the start of every function.")
	flag.Parse()

	// TODO: Proper arguments later when we need some
	args := flag.Args()
	if len(args) != 1 {
		fmt.Println("Invalid arguments")
		return
	}

	filename := args[0]

	f, err := os.Open(filename)

	if err != nil {
		fmt.Printf("Error: Failed to read file: %v\n", err)
		return
	}

	if len(OUTPUT_FILE) == 0 {
		OUTPUT_FILE = fmt.Sprintf("%s.d.pog", filename)
	}

	fmt.Printf("Decompiling package: %s\n", filename)

	if len(INCLUDES_DIR) > 0 {
		LoadDeclarationsFromHeaders(INCLUDES_DIR)
	}

	form, err := readSectionHeader(f)

	if err != nil {
		fmt.Printf("Error: Failed to read file header: %v", err)
		return
	}

	if form.identifier != "FORM" {
		fmt.Printf("Error: Unexpected first section: %s", form.identifier)
		return
	}

	// Skip this part for now...
	f.Seek(4, 1)
	form.length -= 4

	err = readSections(f, form.length)

	if err != nil {
		fmt.Printf("Error: Failed to read file: %v", err)
		return
	}

	// See if we should just output assembly
	if ASSEMBLY_ONLY {

		writer, err := createWriter()
		if err != nil {
			fmt.Printf("Error: Failed to write file: %v\n", err)
			return
		}

		OUTPUT_ASSEMBLY = true
		for idx := 0; idx < len(OPERATIONS); idx++ {
			operation := OPERATIONS[idx]
			if operation.opcode == OP_UNKNOWN_3C {
				continue
			}
			if ASSEMBLY_OFFSET_PREFIX {
				writer.Appendf("// 0x%08X ", operation.offset)
			} else {
				writer.Append("// ")
			}
			operation.WriteAssembly(writer)
			writer.Append("\n")
		}
		return
	}

	// Resolve types until no more are resolved
	resolveAllTypes()

	// If we finished resolving everything, but we still have some unknown function parameters, set them to int
	for _, fnc := range DECOMPILED_FUNCS {
		if fnc.declaration.parameters != nil {
			params := *fnc.declaration.parameters
			for ii := range params {
				param := &params[ii]
				if param.typeName == UNKNOWN_TYPE {
					param.typeName = "int"
					param.variable.AddReferencedType("int")
					if param.variable.refCount > 0 {
						fmt.Printf("WARN: Failed to resolve the type for parameter %s of function %s, defaulting to int.\n", param.parameterName, fnc.declaration.GetScopedName())
					}
				}
			}
			// Lock in our header types
			fnc.declaration.autoDetectTypes = false
		}
	}

	// Resolve types one more time now that we have our functions better defined
	resolveAllTypes()

	// Fix functions with unknown return types
	ResolveAllUnknownFunctionReturnTypes()

	resolveAllTypes()

	checkAllCode()

	resolveAllNames()

	// We need to detect the dependencies so we can reorder imports accordingly
	DetectPackageDependencies()

	writer, err := createWriter()
	if err != nil {
		fmt.Printf("Error: Failed to write file: %v\n", err)
		return
	}

	// Start writing the file
	writer.Appendf("package %s;\n\n", EXPORTING_PACKAGE)
	renderPackageImports(writer)
	renderFunctionExports(writer)

	renderEnums(writer)

	// Render the prototypes
	for ii := range DECOMPILED_FUNCS {
		DECOMPILED_FUNCS[ii].RenderPrototype(writer)
	}

	writer.Append("\n")

	// Render the functions
	for ii := range DECOMPILED_FUNCS {
		fnc := DECOMPILED_FUNCS[ii]
		if fnc.declaration.parameters == nil {
			fmt.Printf("ERROR: Unreferenced exported function with no declaration in headers '%s':, cannot determine parameter count and function will not be output.\n", fnc.declaration.GetScopedName())
			continue
		}
		DECOMPILED_FUNCS[ii].Render(writer)
	}
}
