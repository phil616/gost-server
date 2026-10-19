import { describe, expect, it } from "vitest";
import { windowsRegistrationCommand, windowsStatusCommand } from "./registration";

describe("Windows MSI registration commands", () => {
  it("uses the API origin including its port, an absolute executable and literal arguments", () => {
    const script = windowsRegistrationCommand(
      "altascitunnel register --server wss://relay.example:8443 --token abc_123-XYZ",
    );
    expect(script).toContain("Join-Path $env:ProgramFiles 'AltasCITunnel\\altascitunnel.exe'");
    expect(script).toContain("Start-Service -Name AltasCITunnel -ErrorAction Stop");
    expect(script).toContain("$service.WaitForStatus('Running'");
    expect(script).toContain("& $agent register --server 'wss://relay.example:8443' --token 'abc_123-XYZ'");
    expect(script).toContain("if ($LASTEXITCODE -ne 0)");
    expect(script).toContain("& $agent status");
    expect(windowsStatusCommand).not.toContain("register");
    expect(windowsStatusCommand).not.toContain("--token");
  });

  it("keeps shell metacharacters literal and escapes embedded apostrophes", () => {
    const script = windowsRegistrationCommand(
      "altascitunnel register --server wss://relay.example --token a'b;$(`whoami`)",
    );
    expect(script).toContain("--token 'a''b;$(`whoami`)'\n");
  });

  it.each([
    "altascitunnel register --server https://relay.example --token token",
    "altascitunnel register --server wss://relay.example/agent/v1/ws --token token",
    "altascitunnel register --server wss://user:pass@relay.example --token token",
    "altascitunnel register --server wss://relay.example?test=1 --token token",
    "altascitunnel register --server wss://relay.example#fragment --token token",
    "altascitunnel register --server not-a-url --token token",
    "altascitunnel register --server wss://relay.example --token token --extra unsafe",
    "altascitunnel register --server wss://relay.example --token token\nWrite-Host unsafe",
    "altascitunnel register --server wss://relay.example --token \u2019;Write-Host;\u2019",
    "",
  ])("does not offer an executable Windows script for unsupported input: %s", (input) => {
    expect(windowsRegistrationCommand(input)).toBeUndefined();
  });
});
