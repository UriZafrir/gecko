' Opens Gecko from the Start menu without flashing a console window.
dir = CreateObject("Scripting.FileSystemObject").GetParentFolderName(WScript.ScriptFullName)
CreateObject("WScript.Shell").Run """" & dir & "\gecko.exe"" open", 0, False
