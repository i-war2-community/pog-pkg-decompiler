@ECHO OFF
go build

DEL /Q ..\iwar-script\decompiled\f10\*.pog >nul 2>&1
FOR /f %%a IN ('dir /b "..\iwar-script\packages\f10\*.pkg"') DO (
    pog-pkg-decompiler.exe --assembly=true --includes "..\iwar-script\packages\f10\include" --output "..\iwar-script\decompiled\f10\%%a.pog" "..\iwar-script\packages\f10\%%a"
)

DEL /Q ..\iwar-script\decompiled\f14\*.pog >nul 2>&1
FOR /f %%a IN ('dir /b "..\iwar-script\packages\f14\*.pkg"') DO (
    pog-pkg-decompiler.exe --assembly=false --includes "..\iwar-script\packages\f14\include" --output "..\iwar-script\decompiled\f14\%%a.pog" "..\iwar-script\packages\f14\%%a"
)