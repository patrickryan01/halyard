function Rpc($cfg, $body, $sid) {
  $h = @{ 'Authorization' = "Bearer $($cfg.token)"; 'Accept' = 'application/json, text/event-stream' }
  if ($sid) { $h['Mcp-Session-Id'] = $sid }
  Invoke-WebRequest -UseBasicParsing -Uri $cfg.url -Method Post -Headers $h -ContentType 'application/json' -Body ($body | ConvertTo-Json -Depth 10 -Compress) -TimeoutSec 90
}
$cfg = Get-Content (Join-Path $env:APPDATA 'halyard\mcp.json') | ConvertFrom-Json
$r = Rpc $cfg @{ jsonrpc = '2.0'; id = 1; method = 'initialize'; params = @{ protocolVersion = '2025-06-18'; capabilities = @{}; clientInfo = @{ name = 'win-proof'; version = '1' } } } $null
$sid = $r.Headers['Mcp-Session-Id']
Rpc $cfg @{ jsonrpc = '2.0'; method = 'notifications/initialized' } $sid | Out-Null
function Tool($name, $arguments) { (Rpc $cfg @{ jsonrpc = '2.0'; id = 2; method = 'tools/call'; params = @{ name = $name; arguments = $arguments } } $sid).Content }
$a = Tool 'add_account_password' @{ kind = 'imap'; email = 'alice@lab.test'; password = 'labpass-alice'; imapHost = '10.0.2.2'; imapPort = 18993; imapSecurity = 'ssl'; smtpHost = '10.0.2.2'; smtpPort = 18465; smtpSecurity = 'ssl'; skipVerify = $true }
$a.Substring(0, [Math]::Min(200, $a.Length))
