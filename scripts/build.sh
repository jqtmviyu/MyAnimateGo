#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bash "$project_root/scripts/prepare-web.sh"
bash "$project_root/scripts/build-backend.sh"
