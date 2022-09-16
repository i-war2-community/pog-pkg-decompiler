# POG Decompiler

This project is provided as-is. It was written with the purpose of being run one time, to convert binary pkg files back to pog script files for the Flux engine.

## Usage

pog-pkg-decompiler.exe --includes _directory-of-h-files_ --output _pog-file-to-output_ _pkg-file-to-decompile_

### Optional Flags

| Flag                      | Default | Description                                                                                              |
| ------------------------- | ------- | -------------------------------------------------------------------------------------------------------- |
| --assembly                | false   | The "assmebly" of the original package should be output as comments above each function.                 |
| --assembly-only           | false   | The "assembly" should be output with no code.                                                            |
| --assembly-offset-prefix  | true    | The "assembly" should be prefixed with the byte offset of it's location in the CODE section of the pkg.  |