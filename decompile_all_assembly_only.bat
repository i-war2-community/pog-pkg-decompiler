@ECHO OFF
go build

DEL /Q ..\iwar-script\decompiled\assembly\f10\*.pog >nul 2>&1

FOR /f %%a IN ('dir /b "..\iwar-script\packages\f10\*.pkg"') DO pog-pkg-decompiler.exe --assembly-only=true --assembly-offset-prefix=false --includes "..\iwar-script\include" --output "..\iwar-script\decompiled\f10\assembly\%%a.pog" "..\iwar-script\packages\f10\%%a"

DEL /Q ..\iwar-script\decompiled\assembly\f14\*.pog >nul 2>&1

FOR /f %%a IN ('dir /b "..\iwar-script\packages\f14\*.pkg"') DO pog-pkg-decompiler.exe --assembly-only=true --assembly-offset-prefix=false --includes "..\iwar-script\include" --output "..\iwar-script\decompiled\f14\assembly\%%a.pog" "..\iwar-script\packages\f14\%%a"