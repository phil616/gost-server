// Quote literal PowerShell arguments: double quotes would expand $ and backticks.
function powershellLiteral(value: string) {
  return `'${value.replace(/'/g, "''")}'`;
}

export const windowsStatusCommand = [
  "$agent = Join-Path $env:ProgramFiles 'AltasCITunnel\\altascitunnel.exe'",
  "Get-Service -Name AltasCITunnel -ErrorAction Stop",
  "& $agent status",
].join("\n");

export function windowsRegistrationCommand(command: string): string | undefined {
  // The API provides the configured public origin, which may differ from the
  // browser origin. Only accept the current CLI shape; never execute shell text.
  const match = /^altascitunnel register --server (\S+) --token (\S+)$/.exec(command);
  if (!match) return undefined;
  const [, server, token] = match;
  try {
    const url = new URL(server);
    if (
      url.protocol !== "wss:" || !url.hostname || url.username || url.password ||
      url.search || url.hash || url.pathname !== "/" ||
      /[\x00-\x20\x7f\u2018\u2019]/.test(server + token)
    ) return undefined;
  } catch {
    return undefined;
  }
  return [
    "$ErrorActionPreference = 'Stop'",
    "$agent = Join-Path $env:ProgramFiles 'AltasCITunnel\\altascitunnel.exe'",
    "if (-not (Test-Path -LiteralPath $agent)) { throw 'AltasCI Tunnel executable not found; check the MSI installation path.' }",
    "$service = Get-Service -Name AltasCITunnel -ErrorAction Stop",
    "if ($service.Status -ne 'Running') { Start-Service -Name AltasCITunnel -ErrorAction Stop }",
    "$service.WaitForStatus('Running', [TimeSpan]::FromSeconds(30))",
    `& $agent register --server ${powershellLiteral(server)} --token ${powershellLiteral(token)}`,
    "if ($LASTEXITCODE -ne 0) { throw 'Registration request failed; inspect the error above.' }",
    "& $agent status",
  ].join("\n");
}
