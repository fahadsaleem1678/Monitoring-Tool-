param(
  [string]$AdminUsername = "admin",
  [string]$AdminPassword = "admin123",
  [string]$ViewerUsername = "viewer",
  [string]$ViewerPassword = "viewer123"
)

$ErrorActionPreference = "Stop"

Push-Location (Join-Path $PSScriptRoot "..")
try {
  $secureUri = Read-Host "Paste Supabase Postgres URI" -AsSecureString
  $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureUri)
  try {
    $databaseUrl = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
  } finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
  }

  if ([string]::IsNullOrWhiteSpace($databaseUrl)) {
    throw "DATABASE_URL is required."
  }

  if ($databaseUrl -notmatch "sslmode=") {
    $separator = "?"
    if ($databaseUrl.Contains("?")) {
      $separator = "&"
    }
    $databaseUrl = "$databaseUrl${separator}sslmode=require"
  }

  $env:DATABASE_URL = $databaseUrl
  $env:ADMIN_USERNAME = $AdminUsername
  $env:ADMIN_PASSWORD = $AdminPassword
  $env:VIEWER_USERNAME = $ViewerUsername
  $env:VIEWER_PASSWORD = $ViewerPassword

  docker run --rm `
    -e DATABASE_URL `
    -e ADMIN_USERNAME `
    -e ADMIN_PASSWORD `
    -e VIEWER_USERNAME `
    -e VIEWER_PASSWORD `
    -v "${PWD}\backend:/src" `
    -w /src `
    golang:1.22-alpine `
    go run ./cmd/migrate
} finally {
  Remove-Item Env:\DATABASE_URL -ErrorAction SilentlyContinue
  Remove-Item Env:\ADMIN_USERNAME -ErrorAction SilentlyContinue
  Remove-Item Env:\ADMIN_PASSWORD -ErrorAction SilentlyContinue
  Remove-Item Env:\VIEWER_USERNAME -ErrorAction SilentlyContinue
  Remove-Item Env:\VIEWER_PASSWORD -ErrorAction SilentlyContinue
  Pop-Location
}
