Start-Process 'mailto:dana@example.com?subject=Hello%20from%20mailto&body=Opened%20by%20the%20Windows%20handler'
Start-Sleep 6
"halyard processes: $((Get-Process halyard -ErrorAction SilentlyContinue | Measure-Object).Count)"
