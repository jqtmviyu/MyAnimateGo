#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
embedded_dir="$project_root/backend/internal/webui/dist"
licenses_dir="$project_root/backend/internal/webui/licenses"

(cd "$project_root/frontend" && pnpm install --frozen-lockfile && pnpm build)
python3 - "$project_root/frontend/dist" "$embedded_dir" "$licenses_dir" <<'PYTHON'
from pathlib import Path
import shutil
import sys

source, embedded, licenses = map(Path, sys.argv[1:])
if not (source / 'index.html').is_file():
    raise SystemExit('前端构建缺少 index.html，停止生成发布程序')
for target in (embedded, licenses):
    if target.exists():
        shutil.rmtree(target)
# 静态服务读取原始文件，省略不参与响应协商的预压缩副本。
shutil.copytree(source, embedded, ignore=shutil.ignore_patterns('*.gz'))
PYTHON
python3 "$project_root/scripts/collect-licenses.py" "$licenses_dir" --only frontend
