param([Parameter(Mandatory)][string]$Bundle)
$ErrorActionPreference = 'Stop'
$Bundle = (Resolve-Path -LiteralPath $Bundle).Path
if (Test-Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\koujou-san') {
    throw 'Use a clean Windows test account; an installed product already exists.'
}
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('koujou-distribution-' + [guid]::NewGuid().ToString('N'))
$installed = Join-Path $fixture 'installed'
New-Item -ItemType Directory -Path $fixture | Out-Null
$setup = @(Get-ChildItem -LiteralPath $Bundle -Filter '*-setup.exe')
if ($setup.Count -ne 1) { throw 'One fixed installer required.' }
if ((Get-AuthenticodeSignature -LiteralPath $setup[0].FullName).Status -ne 'NotSigned') { throw 'Preview signing expectation differs.' }
function Install {
    $process = Start-Process -FilePath $setup[0].FullName -ArgumentList @('/S', "/D=$installed") -WindowStyle Hidden -PassThru -Wait
    return $process.ExitCode
}
function RequireFile([string]$Name) {
    if (-not (Test-Path -LiteralPath (Join-Path $installed $Name))) { throw "Missing installed $Name" }
}
if ((Install) -ne 0) { throw 'Fresh installation failed.' }
$runner = Join-Path $installed 'koujou-san-runner.exe'
$original = Join-Path $fixture 'original-runner.exe'
$fake = $null
try {
    foreach ($name in @('koujou-san-desktop.exe','koujou-san-runner.exe','LICENSE','NOTICE','THIRD_PARTY_NOTICES.json','sources.tar.gz')) { RequireFile $name }
    foreach ($name in @('LICENSE','NOTICE','THIRD_PARTY_NOTICES.json','sources.tar.gz')) {
        if ((Get-FileHash -LiteralPath (Join-Path $installed $name)).Hash -ne (Get-FileHash -LiteralPath (Join-Path $Bundle $name)).Hash) { throw "Installed $name differs" }
    }
    if ((Get-FileHash -LiteralPath $runner).Hash -ne (Get-FileHash -LiteralPath (Join-Path $Bundle 'binaries/koujou-san-runner.exe')).Hash) { throw 'Installed Runner differs' }
    Copy-Item -LiteralPath $runner -Destination $original
    $source = Join-Path $fixture 'idle-fixture.cs'
    [IO.File]::WriteAllText($source, 'class OwnedRunnerFixture { static void Main() { System.Threading.Thread.Sleep(120000); } }')
    $compiler = Join-Path $env:WINDIR 'Microsoft.NET/Framework64/v4.0.30319/csc.exe'
    & $compiler /nologo /target:winexe "/out:$runner" $source
    if ($LASTEXITCODE -ne 0) { throw 'Owned process fixture compile failed' }
    $fake = Start-Process -FilePath $runner -WindowStyle Hidden -PassThru
    Start-Sleep -Milliseconds 300
    if ((Install) -ne 2) { throw 'Installer did not reject an active Runner' }
    $fake.Refresh()
    if ($fake.HasExited) { throw 'Installer killed the owned Runner fixture' }
    Stop-Process -Id $fake.Id
    $fake.WaitForExit()
    Copy-Item -LiteralPath $original -Destination $runner -Force
    if ((Install) -ne 0) { throw 'Manual reinstall failed after Runner exited' }
    Write-Output 'PASS: unsigned NSIS installation, bundled Runner/notices/sources, active Runner preservation and manual reinstall'
} finally {
    if ($null -ne $fake) {
        $fake.Refresh()
        if (-not $fake.HasExited) { Stop-Process -Id $fake.Id; $fake.WaitForExit() }
    }
    $uninstaller = Join-Path $installed 'uninstall.exe'
    if (Test-Path -LiteralPath $uninstaller) {
        $process = Start-Process -FilePath $uninstaller -ArgumentList '/S' -WindowStyle Hidden -PassThru -Wait
        if ($process.ExitCode -ne 0) { throw 'Owned installation cleanup failed' }
        for ($attempt = 0; $attempt -lt 100 -and (Test-Path -LiteralPath $runner); $attempt++) { Start-Sleep -Milliseconds 100 }
        if (Test-Path -LiteralPath $runner) { throw 'Owned Runner remained after uninstall' }
    }
    Write-Output "Owned installer fixture retained at $fixture"
}
