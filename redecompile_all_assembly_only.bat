@ECHO OFF
go build

REM DEL /Q ..\iwar-script\redecompiled\assembly\*.pog >nul 2>&1

FOR /f %%a IN ('dir /b "..\iwar-script\decompiled\*.pkg"') DO pog-pkg-decompiler.exe --assembly-only=true --assembly-offset-prefix=false --includes "..\iwar-script\include" --output "..\iwar-script\redecompiled\assembly\%%a.pog" "..\iwar-script\decompiled\%%a"