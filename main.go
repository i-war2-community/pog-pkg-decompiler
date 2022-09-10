package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"
)

// Command line options
var INCLUDES_DIR string
var OUTPUT_ASSEMBLY bool

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
	results := make([]string, len(imports))

	copy(results, imports)

	sort.Slice(results, func(a, b int) bool {
		pkgA := PACKAGES[strings.ToLower(results[a])]
		pkgB := PACKAGES[strings.ToLower(results[b])]

		_, dependsBA := pkgB.dependencies[pkgA.name]
		_, dependsAB := pkgA.dependencies[pkgB.name]

		return dependsBA && !dependsAB
	})

	return results
}

func renderPackageImports(writer CodeWriter) {
	importCount := len(PACKAGE_IMPORTS)
	if importCount == 0 {
		return
	}

	imports := sortPackageImports(PACKAGE_IMPORTS)

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
		return nil, fmt.Errorf("Header not long enough")
	}

	result.identifier = string(buffer)

	// Read the section length
	result.length, err = readUInt32BigEndian(file)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func readSections(file *os.File, maximumLength uint32, writer CodeWriter) error {

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

		err = readSection(file, section, writer)
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

func readSection(file *os.File, section *SectionHeader, writer CodeWriter) error {
	var err error = nil

	switch section.identifier {
	case "PIMP":
		name, err := readString(file)
		if err != nil {
			return err
		}
		//fmt.Printf("%s", name)
		if name != "__system" {
			_, ok := PACKAGES[name]
			if !ok {
				fmt.Printf("ERROR: Importing package '%s' not found in includes!", name)
				os.Exit(1)
			}
			// Get the package name with the correct upper and lower case letters
			name = PACKAGES[name].name

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
			fmt.Printf("ERROR: Exporting package '%s' not found in includes!", name)
			os.Exit(1)
		}
		EXPORTING_PACKAGE = lookup.name

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
		err = readCodeSection(file, writer)
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

func readCodeSection(file *os.File, writer CodeWriter) error {

	buffer := make([]byte, 4)
	n, err := file.Read(buffer)
	if err != nil {
		return err
	}
	if n != 4 {
		return fmt.Errorf("Code not long enough")
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
			return fmt.Errorf("Error: Unknown opcode 0x%02X at position 0x%08X\n", opcode, offset+uint32(initialOffset))
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
		operation := OPERATIONS[idx]
		declaration := FUNC_DEFINITION_MAP[operation.offset]

		if declaration != nil {
			var def *FunctionDefinition
			idx, def = DecompileFunction(declaration, idx, initialOffset, writer)
			DECOMPILED_FUNCS = append(DECOMPILED_FUNCS, def)
		}
	}

	return nil
}

func main() {
	flag.StringVar(&INCLUDES_DIR, "includes", "", "The includes directory with package headers.")
	flag.BoolVar(&OUTPUT_ASSEMBLY, "assembly", false, "Have the decompiler output the 'assembly' for each function as comments above the function.")
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
		fmt.Printf("Error: Failed to read file: %v", err)
		return
	}

	if len(INCLUDES_DIR) > 0 {
		LoadFunctionDeclarationsFromHeaders(INCLUDES_DIR)
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

	// TODO: Move this somewhere else
	writer := NewCodeWriter()

	err = readSections(f, form.length, writer)

	if err != nil {
		fmt.Printf("Error: Failed to read file: %v", err)
		return
	}

	// Resolve types until no more are resolved
	for {
		resolveCount := 0
		for ii := range DECOMPILED_FUNCS {
			resolveCount += DECOMPILED_FUNCS[ii].ResolveTypes()
		}
		if resolveCount == 0 {
			break
		}
	}

	// Fix functions with unknown return types
	SetAllUnknownFunctionReturnTypesToVoid()

	// We need to detect the dependencies so we can reorder imports accordingly
	DetectPackageDependencies()

	writer.Appendf("package %s;\n\n", EXPORTING_PACKAGE)
	renderPackageImports(writer)
	renderFunctionExports(writer)

	// Render the prototypes
	for ii := range DECOMPILED_FUNCS {
		DECOMPILED_FUNCS[ii].RenderPrototype(writer)
	}

	writer.Append("\n")

	// Render the functions
	for ii := range DECOMPILED_FUNCS {
		DECOMPILED_FUNCS[ii].Render(writer)
	}

	output := fmt.Sprintf("%s.d.pog", filename)

	results := writer.Bytes()

	err = ioutil.WriteFile(output, results, 0644)
	if err != nil {
		fmt.Printf("Error: Failed to write file: %v", err)
		return
	}
	fmt.Print(writer.String())

}
