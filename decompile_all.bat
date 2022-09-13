@ECHO OFF
go build

DEL /Q ..\iwar-script\decompiled\*.pog >nul 2>&1

FOR /f %%a IN ('dir /b "..\iwar-script\packages\*.pkg"') DO pog-pkg-decompiler.exe --assembly=false --includes "..\iwar-script\include" --output "..\iwar-script\decompiled\%%a.pog" "..\iwar-script\packages\%%a"