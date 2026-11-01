#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${MYANIMEGO_OUTPUT_DIR:-$project_root/dist}"
frontend_licenses="$project_root/backend/internal/webui/licenses"

if [[ ! -f "$project_root/backend/internal/webui/dist/index.html" || ! -f "$frontend_licenses/frontend-dependencies.json" ]]; then
    printf '缺少前端资源或许可证，请先执行 bash scripts/prepare-web.sh\n' >&2
    exit 1
fi
mkdir -p "$output_dir"
output_dir="$(cd "$output_dir" && pwd)"
python3 - "$output_dir" <<'PYTHON'
from pathlib import Path
import shutil
import sys

for name in ('web', 'LICENSES'):
    target = Path(sys.argv[1]) / name
    if target.exists():
        shutil.rmtree(target)
PYTHON
program_name=myanimego
if [[ "$(go env GOOS)" == windows ]]; then program_name=myanimego.exe; fi
(cd "$project_root/backend" && go build -tags webui -buildvcs=false -trimpath -o "$output_dir/$program_name" ./cmd/myanimego)
cp "$project_root/LICENSE" "$project_root/README.md" "$output_dir/"
cp -R "$project_root/LICENSES" "$output_dir/LICENSES"
cp -R "$frontend_licenses/." "$output_dir/LICENSES/"
cp "$project_root/backend/internal/apidocs/assets/LICENSE" "$output_dir/LICENSES/Swagger-UI-Apache-2.0.txt"
cp "$project_root/backend/internal/apidocs/assets/swagger-ui-bundle.js.LICENSE.txt" "$output_dir/LICENSES/Swagger-UI-dependencies.txt"
python3 "$project_root/scripts/collect-licenses.py" "$output_dir/LICENSES" --only go
printf '构建完成：%s\n' "$output_dir"
