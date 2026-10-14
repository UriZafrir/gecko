# Adds or removes a folder in the user's PATH: path.ps1 add|remove <dir>
# Reads and writes the registry value as is, so entries like
# %USERPROFILE%\bin stay unexpanded.
param([string]$Action, [string]$Dir)
$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$old = $key.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
$parts = @($old -split ';' | Where-Object { $_ -and $_.TrimEnd('\') -ne $Dir.TrimEnd('\') })
if ($Action -eq 'add') { $parts += $Dir }
$new = $parts -join ';'
if ($new -ne $old) { $key.SetValue('Path', $new, 'ExpandString') }
$key.Close()
