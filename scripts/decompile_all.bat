@ECHO OFF
go build

IF NOT EXIST "output" mkdir "output"
IF NOT EXIST "output\decompiled" mkdir "output\decompiled"

FOR /f %%a IN ('dir /b "packages\*.pkg"') DO (
    pog-pkg-decompiler --assembly=true --includes "packages\include" --output "output\decompiled\%%a.pog" "packages\%%a"
)