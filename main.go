package main

import (
	"flag"
	"fmt"
	"pog-pkg-decompiler/decompiler"
)

func main() {
	flag.StringVar(&decompiler.INCLUDES_DIR, "includes", "", "The includes directory with package headers.")
	flag.StringVar(&decompiler.OUTPUT_FILE, "output", "", "The file path to which the pog file will be written.")
	flag.BoolVar(&decompiler.OUTPUT_ASSEMBLY, "assembly", false, "Have the decompiler output the 'assembly' for each function as comments above the function.")
	flag.BoolVar(&decompiler.ASSEMBLY_ONLY, "assembly-only", false, "Have the decompiler output only the assembly for the package.")
	flag.BoolVar(&decompiler.ASSEMBLY_OFFSET_PREFIX, "assembly-offset-prefix", true, "Prefix each line of assembly with its binary address.")
	flag.BoolVar(&decompiler.DEBUG_LOGGING, "debug", false, "Output code that logs debug info at the start of every function.")
	flag.Parse()

	// TODO: Proper arguments later when we need some
	args := flag.Args()
	if len(args) != 1 {
		fmt.Println("Invalid arguments")
		return
	}
	decompiler.INPUT_FILE = args[0]

	decompiler.Decompile()
}
