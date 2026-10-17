# CI smoke test of the Windows setup: installs it silently (per user), starts
# Gecko with a fresh GECKO_HOME and a spare port, checks that it started its
# bundled daemon and that the UI loads, types into a terminal, closes the
# window, relaunches and checks the session survived. Screenshots and video
# frames go to $Out.
#   pwsh packaging/desktop/smoke/windows.ps1 dist\desktop\Gecko-...-setup.exe out
param([string]$Setup, [string]$Out = "out")
$ErrorActionPreference = "Continue"
$Port = 17695
New-Item -ItemType Directory -Force -Path "$Out\frames" | Out-Null
$Out = (Resolve-Path $Out).Path
$env:GECKO_HOME = Join-Path $env:RUNNER_TEMP "gecko-home"
Remove-Item -Recurse -Force $env:GECKO_HOME -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $env:GECKO_HOME | Out-Null
"{`"listen`":`"127.0.0.1:$Port`",`"name`":`"windows-ci`"}" | Set-Content -Encoding ascii "$env:GECKO_HOME\config.json"
$script:fail = 0
function Log($m) { $l = "[{0}] {1}" -f (Get-Date -Format HH:mm:ss), $m; Write-Host $l; Add-Content "$Out\smoke.log" $l }
function Check($name, [scriptblock]$b) { if (& $b) { Log "PASS $name" } else { Log "FAIL $name"; $script:fail = 1 } }

Add-Type -AssemblyName System.Windows.Forms, System.Drawing
function Shot($path) {
  $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
  $bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
  $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
  $g.Dispose(); $bmp.Dispose()
}

Log "installing $Setup"
$p = Start-Process -FilePath $Setup -ArgumentList "/S" -Wait -PassThru
Log "setup exit code $($p.ExitCode)"
$exe = Get-ChildItem -Path "$env:LOCALAPPDATA\Programs" -Recurse -Filter Gecko.exe -ErrorAction SilentlyContinue | Where-Object { $_.DirectoryName -notlike "*resources*" } | Select-Object -First 1
Check "installed Gecko.exe" { $null -ne $exe }
$App = $exe.FullName
$G = Join-Path $exe.DirectoryName "resources\gecko.exe"
Log "app: $App"
Log "bundled gecko: $(& $G version)"

# Video frames, two a second, in the background.
$frames = Start-Job -ArgumentList "$Out\frames", "$Out\.stop" -ScriptBlock {
  param($dir, $stop)
  Add-Type -AssemblyName System.Windows.Forms, System.Drawing
  $i = 0
  while (-not (Test-Path $stop)) {
    $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
    $bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
    $bmp.Save(("{0}\{1:D4}.png" -f $dir, $i), [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose(); $i++
    Start-Sleep -Milliseconds 400
  }
}

function Launch {
  $script:proc = Start-Process -FilePath $App -PassThru
  for ($i = 0; $i -lt 60 -and -not (Test-Path "$env:GECKO_HOME\state\web"); $i++) { Start-Sleep -Milliseconds 500 }
}

Log "first launch"
Launch
Start-Sleep 8
$token = (Get-Content "$env:GECKO_HOME\token").Trim()
$hdr = @{ Authorization = "Bearer $token" }
Check "daemon answers /api/info" {
  try { $r = Invoke-RestMethod -Headers $hdr "http://127.0.0.1:$Port/api/info"; Log ($r | ConvertTo-Json -Compress); $r.host -eq "windows-ci" } catch { Log $_; $false }
}
$daemon = Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like "*gecko-*.exe*daemon*" }
Log "daemon: $($daemon.CommandLine)"
Check "daemon runs a copy of the bundled gecko" { $null -ne $daemon }
Check "web UI served" { (Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:$Port/").Content -match "<title>Gecko" }
Shot "$Out\windows-1-start.png"

$S = (& $G new -n smoke).Trim()
Log "session: $S"
Start-Sleep 3
# Keystrokes into the window, then gecko send as the dependable path.
$ws = New-Object -ComObject WScript.Shell
$typed = $ws.AppActivate($script:proc.Id)
Start-Sleep 1
if ($typed) { [System.Windows.Forms.SendKeys]::SendWait('echo "typed in the Gecko window"; $env:MARK = 42{ENTER}') }
Log "keystrokes through the window: $typed"
Start-Sleep 2
& $G send $S 'echo "hello from gecko send on $([Environment]::OSVersion.VersionString)"; $env:MARK2 = 43'
Start-Sleep 4
Shot "$Out\windows-2-typed.png"
& $G capture $S | Set-Content "$Out\capture-1.txt"
Check "terminal ran the command" { (Get-Content -Raw "$Out\capture-1.txt") -match "hello from gecko send on" }

Log "close the window"
$script:proc.CloseMainWindow() | Out-Null
Start-Sleep 3
if (-not $script:proc.HasExited) { Stop-Process -Id $script:proc.Id -Force }
Start-Sleep 2
Check "window gone, daemon still up" {
  try { $null -ne (Invoke-RestMethod -Headers $hdr "http://127.0.0.1:$Port/api/info") } catch { $false }
}

Log "relaunch"
Launch
Start-Sleep 8
& $G send $S 'echo "same shell after relaunch: MARK2=$env:MARK2"'
Start-Sleep 4
Shot "$Out\windows-3-relaunch.png"
& $G capture $S | Set-Content "$Out\capture-2.txt"
Check "session survived the relaunch" { (Get-Content -Raw "$Out\capture-2.txt") -match "same shell after relaunch: MARK2=43" }
New-Item -ItemType File "$Out\.stop" | Out-Null
Wait-Job $frames -Timeout 10 | Out-Null
Get-Process -Name Gecko -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $App } | Stop-Process -Force
& $G stop
Copy-Item "$env:GECKO_HOME\state\daemon.log" $Out -ErrorAction SilentlyContinue
exit $script:fail
