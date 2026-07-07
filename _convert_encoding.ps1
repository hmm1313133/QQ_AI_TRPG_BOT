$path = "start.ps1"
$content = Get-Content -Path $path -Raw -Encoding UTF8
$utf8Bom = New-Object System.Text.UTF8Encoding $true
[System.IO.File]::WriteAllText((Resolve-Path $path).Path, $content, $utf8Bom)
Write-Host "Done: converted to UTF-8 with BOM"
