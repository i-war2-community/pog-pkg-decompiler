set -e
go build

for FILE in test/packages/*.pkg; do ./pog-pkg-decompiler --assembly=false --includes "test/include" --output "test/decompiled/$(basename $FILE).pog" $FILE; done