@ECHO OFF
go build

IF NOT EXIST "output" mkdir "output"
IF NOT EXIST "output\decompiled" mkdir "output\decompiled"
IF NOT EXIST "output\decompiled\assembly" mkdir "output\decompiled\assembly"

FOR /f %%a IN ('dir /b "packages\*.pkg"') DO (
    pog-pkg-decompiler.exe --assembly-only=true --assembly-offset-prefix=false --includes "packages\include" --output "output\decompiled\assembly\%%a.pog" "packages\%%a"
)