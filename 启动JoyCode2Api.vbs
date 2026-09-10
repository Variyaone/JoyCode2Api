Set fso = CreateObject("Scripting.FileSystemObject")
Set shell = CreateObject("WScript.Shell")
Dim p, exe
p = fso.GetParentFolderName(WScript.ScriptFullName)
exe = p & "\JoyCode2Api.exe"
shell.CurrentDirectory = p
shell.Run Chr(34) & exe & Chr(34) & " daemon start --port 34891 --skip-validation", 0, True
