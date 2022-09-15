@ECHO OFF
go build

@REM DEL /Q ..\iwar-script\redecompiled\f10\assembly\*.pog >nul 2>&1

@REM FOR /f %%a IN ('dir /b "..\iwar-script\decompiled\f10\packages\*.pkg"') DO pog-pkg-decompiler.exe --assembly-only=true --assembly-offset-prefix=false --includes "..\iwar-script\include" --output "..\iwar-script\redecompiled\f10\assembly\%%a.pog" "..\iwar-script\decompiled\f10\packages\%%a"

DEL /Q ..\iwar-script\redecompiled\f14\assembly\*.pog >nul 2>&1

FOR /f %%a IN ('dir /b "..\iwar-script\decompiled\f14\packages\*.pkg"') DO pog-pkg-decompiler.exe --assembly-only=true --assembly-offset-prefix=false --includes "..\iwar-script\include" --output "..\iwar-script\redecompiled\f14\assembly\%%a.pog" "..\iwar-script\decompiled\f14\packages\%%a"