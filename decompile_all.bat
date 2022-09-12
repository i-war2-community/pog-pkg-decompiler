@ECHO OFF
go build
IF NOT EXIST output MKDIR output
FOR /f %%a IN ('dir /b "..\iwar-script\packages\*.pkg"') DO pog-pkg-decompiler.exe --assembly=false --includes "..\iwar-script\include" --output "..\iwar-script\decompiled\%%a.pog" "..\iwar-script\packages\%%a"