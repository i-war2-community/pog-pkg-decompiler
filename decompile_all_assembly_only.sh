set -e
go build
mkdir -p test/decompiled/assembly
for FILE in test/packages/*.pkg; do ./pog-pkg-decompiler --assembly-only=true --assembly-offset-prefix=false --includes "test/include" --output "test/decompiled/assembly/$(basename $FILE).pog" $FILE; done