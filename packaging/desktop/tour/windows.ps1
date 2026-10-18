# The feature tour on a Windows CI runner (after smoke/windows.ps1 installed
# Gecko): a fresh GECKO_HOME, the screen recorded with ffmpeg gdigrab, the
# tour driven by tour.cjs. The second machine is a second Gecko daemon on
# the same runner (its own GECKO_HOME and port) connected with gecko bridge:
# the runner has no sshd. The agents chapter is skipped: the Windows daemon
# has no process inspection yet, so it can't tell an agent is running.
# Writes $Out\tour.mkv, captions.txt and t0.txt.
#   pwsh packaging/desktop/tour/windows.ps1 -Out out\tour
param([string]$Out = "out\tour")
$ErrorActionPreference = "Continue"
New-Item -ItemType Directory -Force -Path $Out | Out-Null
$Out = (Resolve-Path $Out).Path
$Here = $PSScriptRoot
$Port = 17696; $Cdp = 17699
function Log($m) { $l = "[{0}] {1}" -f (Get-Date -Format HH:mm:ss), $m; Write-Host $l; Add-Content "$Out\tour-setup.log" $l }

$exe = Get-ChildItem -Path "$env:LOCALAPPDATA\Programs" -Recurse -Filter Gecko.exe -ErrorAction SilentlyContinue | Where-Object { $_.DirectoryName -notlike "*resources*" } | Select-Object -First 1
$App = $exe.FullName
$Res = Join-Path $exe.DirectoryName "resources"
$G = Join-Path $Res "gecko.exe"
Log "app: $App"

$Home2 = Join-Path $env:RUNNER_TEMP "tour-second"
New-Item -ItemType Directory -Force -Path $Home2 | Out-Null
'{"listen":"127.0.0.1:17697","name":"second"}' | Set-Content -Encoding ascii "$Home2\config.json"
$env:GECKO_HOME = Join-Path $env:RUNNER_TEMP "tour-home"
Remove-Item -Recurse -Force $env:GECKO_HOME -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path "$env:GECKO_HOME\state" | Out-Null
# The transport: a batch file, so no quoting has to survive cmd.exe.
$bridge = Join-Path $env:RUNNER_TEMP "second-bridge.cmd"
"@set GECKO_HOME=$Home2`r`n@`"$G`" bridge`r`n" | Set-Content -Encoding ascii -NoNewline $bridge
$cfg = @{ listen = "0.0.0.0:$Port"; name = "windows-ci"; hosts = @(@{ name = "second"; command = @("cmd.exe", "/d", "/c", $bridge) }) }
$cfg | ConvertTo-Json -Depth 5 | Set-Content -Encoding ascii "$env:GECKO_HOME\config.json"
'{"theme":"tokyo-night","fontSize":13,"opacity":1}' | Set-Content -Encoding ascii "$env:GECKO_HOME\settings.json"
New-Item -ItemType File -Force "$env:GECKO_HOME\no-login-service" | Out-Null
Get-Content "$env:GECKO_HOME\config.json" | ForEach-Object { Log $_ }

Push-Location $Here; npm ci --no-audit --no-fund --silent; Pop-Location

$ff = (Get-Command ffmpeg -ErrorAction SilentlyContinue).Source
if (-not $ff) { choco install ffmpeg -y --no-progress | Out-Null; $ff = (Get-Command ffmpeg -ErrorAction SilentlyContinue).Source }
if (-not $ff) { $c = "C:\ProgramData\chocolatey\bin\ffmpeg.exe"; if (Test-Path $c) { $ff = $c } }
Log "ffmpeg: $ff"

# Behind the window, for the transparency chapter: a photo wallpaper and a
# Chrome window with big text, opened before Gecko so Gecko is on top.
# Transparency effects on, as on most desktops.
$chrome = @("C:\Program Files\Google\Chrome\Application\chrome.exe", "C:\Program Files (x86)\Google\Chrome\Application\chrome.exe") | Where-Object { Test-Path $_ } | Select-Object -First 1
$pers = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize"
New-Item -Force -Path $pers -ErrorAction SilentlyContinue | Out-Null
Set-ItemProperty -Path $pers -Name EnableTransparency -Value 1 -Type DWord -ErrorAction SilentlyContinue
$wall = (Resolve-Path (Join-Path $Here "..\..\..\web\public\themes\jellyfish.jpg")).Path
Add-Type -TypeDefinition 'using System.Runtime.InteropServices; public class Wall { [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern int SystemParametersInfo(int a, int b, string c, int d); }'
Log ("wallpaper: {0}" -f [Wall]::SystemParametersInfo(20, 0, $wall, 3))
Add-Type -TypeDefinition 'using System; using System.Runtime.InteropServices; public class Bcast { [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern IntPtr SendMessageTimeout(IntPtr h, int m, IntPtr w, string l, int f, int t, out IntPtr r); }'
$r = [IntPtr]::Zero
[Bcast]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [IntPtr]::Zero, "ImmersiveColorSet", 2, 3000, [ref]$r) | Out-Null
Log ("transparency effects: {0}" -f (Get-ItemProperty -Path $pers -Name EnableTransparency -ErrorAction SilentlyContinue).EnableTransparency)
Log ("windows build: {0}" -f [Environment]::OSVersion.Version)
if ($chrome) {
  $bd = (Join-Path $Here "backdrop.html") -replace '\\', '/'
  Start-Process -FilePath $chrome -ArgumentList "--user-data-dir=`"$env:RUNNER_TEMP\backdrop`" --no-first-run --no-default-browser-check --app=file:///$bd --window-position=20,40 --window-size=560,440" | Out-Null
  Start-Sleep 5
}

"{0}|start" -f [DateTimeOffset]::UtcNow.ToUnixTimeSeconds() | Set-Content "$Out\t0.txt"
$rec = Start-Process -FilePath $ff -ArgumentList "-y -loglevel error -f gdigrab -framerate 15 -draw_mouse 0 -i desktop -c:v libx264 -preset veryfast -tune zerolatency -crf 22 -pix_fmt yuv420p -flush_packets 1 `"$Out\tour.mkv`"" -PassThru -WindowStyle Hidden
Start-Sleep 2

$env:PATH = "$Res;$env:PATH"
$proj = Join-Path $env:RUNNER_TEMP "webapp"
New-Item -ItemType Directory -Force -Path $proj | Out-Null
Push-Location $proj
$app = Start-Process -FilePath $App -ArgumentList "--remote-debugging-port=$Cdp" -PassThru
Pop-Location
for ($i = 0; $i -lt 60; $i++) { try { Invoke-RestMethod "http://127.0.0.1:$Cdp/json/version" | Out-Null; break } catch { Start-Sleep -Milliseconds 500 } }
Start-Sleep 4

$env:TOUR_CDP = "$Cdp"; $env:TOUR_PLATFORM = "windows"; $env:TOUR_CAPTIONS = "$Out\captions.txt"
$env:TOUR_BOUNDS = "0,0,1024,728"
if ($chrome) { $env:TOUR_PHONE_BROWSER = $chrome }
$env:TOUR_PHONE_PROFILE = Join-Path $env:RUNNER_TEMP "phone"
$env:TOUR_PHONE_LABEL = "a second client (Chrome, sized like a phone)"
$env:TOUR_REMOTE_NAME = "second"; $env:TOUR_REMOTE_ADDED = "1"
$env:TOUR_REMOTE_LABEL = "a second Gecko daemon on this runner (own home and port), connected with gecko bridge"
$env:TOUR_AGENTS = "0"
$env:TOUR_OUTSIDE = "1"; $env:TOUR_GECKO = $G
$env:TOUR_AGENTS_WHY = "skipped on Windows: the Windows daemon can't inspect processes yet, so it doesn't detect agents"
node "$Here\tour.cjs" 2>&1 | Tee-Object -FilePath "$Out\tour.log"
$rc = $LASTEXITCODE
Start-Sleep 4
Stop-Process -Id $rec.Id -ErrorAction SilentlyContinue
Start-Sleep 2
Get-Process -Name Gecko -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $App } | Stop-Process -Force
& $G stop
$env:GECKO_HOME = $Home2; & $G stop
Get-ChildItem $Out | ForEach-Object { Log "$($_.Name) $($_.Length)" }
exit $rc
