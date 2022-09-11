@ECHO OFF
go build
IF NOT EXIST output MKDIR output
FOR /f %%a IN ('dir /b "..\iwar-script\packages\*.pkg"') DO pog-pkg-decompiler.exe --includes "..\iwar-script\include" "..\iwar-script\packages\%%a"