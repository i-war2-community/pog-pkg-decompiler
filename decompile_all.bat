@ECHO OFF
go build
FOR /f %%a IN ('dir /b "C:\Program Files (x86)\Infogrames\Independence War 2 - Edge of Chaos\resource\packages\*.pkg"') DO pog-pkg-decompiler.exe "C:\Program Files (x86)\Infogrames\Independence War 2 - Edge of Chaos\resource\packages\%%a" > .\output\%%a.log