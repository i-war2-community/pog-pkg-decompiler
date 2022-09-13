@ECHO OFF
go build

REM DEL /Q ..\iwar-script\decompiled\assembly\*.pog >nul 2>&1

FOR /f %%a IN ('dir /b "..\iwar-script\packages\*.pkg"') DO pog-pkg-decompiler.exe --assembly-only=true --assembly-offset-prefix=false --includes "..\iwar-script\include" --output "..\iwar-script\decompiled\assembly\%%a.pog" "..\iwar-script\packages\%%a"