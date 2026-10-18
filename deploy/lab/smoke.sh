#!/bin/sh
set -eu
cd "$(dirname "$0")"
# pwsh runs the identical scenario suite on Linux; no independent allocator.
# Install PowerShell 7 on the test workstation, not on each deployed Edge.
command -v pwsh >/dev/null || { echo 'NOT_TESTED: full smoke requires pwsh; use README HTTP/network checks on an Edge' >&2; exit 1; }
exec pwsh -NoProfile -File ./smoke.ps1 "$@"
