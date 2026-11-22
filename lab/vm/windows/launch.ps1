Start-Process 'C:\Program Files\Halyard\halyard.exe'
Start-Sleep 20
"running: $((Get-Process halyard -ErrorAction SilentlyContinue | Measure-Object).Count) process(es)"
"mcp.json: $(Test-Path (Join-Path $env:APPDATA 'halyard\mcp.json'))"
