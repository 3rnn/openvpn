#!/bin/bash
# Retired: the legacy installer downloaded unreviewed executables and modified
# host services/firewall settings. It must not be used, including for upgrades.
set -euo pipefail
printf '%s\n' 'The host installer is retired. Build the reviewed source using build/Dockerfile from the repository root; see README.md.' >&2
exit 1
