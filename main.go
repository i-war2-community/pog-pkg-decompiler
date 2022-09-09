package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

var EXPORTING_PACKAGE string
var IMPORTING_PACKAGE string

var FUNC_EXPORTS []*FunctionDeclaration = []*FunctionDeclaration{}

var FUNC_DEFINITION_MAP map[uint32]*FunctionDeclaration = map[uint32]*FunctionDeclaration{}
var FUNC_IMPORT_MAP map[uint32]*FunctionDeclaration = map[uint32]*FunctionDeclaration{}

var STRING_TABLE = []string{}
var OPERATIONS = []Operation{}

type SectionHeader struct {
	identifier string
	length     uint32
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

	// TODO: Move this somewhere else
	writer := NewCodeWriter()

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

	// TEMPORARY OUTPUT
	for idx := 0; idx < len(OPERATIONS); idx++ {
		operation := OPERATIONS[idx]
		declaration := FUNC_DEFINITION_MAP[operation.offset]

		if declaration != nil {
			idx = DecompileFunction(declaration, idx, initialOffset, writer)
		}
	}

	fmt.Print(writer.String())

	return nil
}

func main() {
	//filename := "iacttwo.pkg"
	//filename := "..\\iwar-script\\packages\\iact2mission25.pkg"
	//filename := "..\\iwar-script\\packages\\test.pkg"

	// TODO: Proper arguments later when we need some
	args := os.Args[1:]
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
}
