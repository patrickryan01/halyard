# What the registered handler does: run the command stored under Halyard.Url.mailto with the link in place of %1.
$cmd = (Get-ItemProperty 'HKLM:\Software\Classes\Halyard.Url.mailto\shell\open\command').'(default)'
"handler command: $cmd"
$exe = 'C:\Program Files\Halyard\halyard.exe'
$before = (Get-Process halyard -ErrorAction SilentlyContinue | Measure-Object).Count
Start-Process $exe -ArgumentList 'mailto:dana@example.com?subject=Hello%20from%20mailto&body=Opened%20by%20the%20Windows%20handler'
Start-Sleep 8
$after = (Get-Process halyard -ErrorAction SilentlyContinue | Measure-Object).Count
"halyard processes before $before, after $after (the second launch hands the link to the running window and exits)"
