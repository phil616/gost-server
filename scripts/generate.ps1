$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate
if ($LASTEXITCODE -ne 0) {throw 'sqlc generation failed'}
