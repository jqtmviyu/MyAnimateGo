#!/usr/bin/env python3
"""将实际前端依赖和 Go 构建依赖的许可随二进制一起打包。"""
import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess

project_root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('output', type=Path)
parser.add_argument('--only', choices=('all', 'frontend', 'go'), default='all')
args = parser.parse_args()
output = args.output.resolve()
output.mkdir(parents=True, exist_ok=True)


def documents(directory):
    return sorted(path for path in directory.iterdir()
                  if path.is_file() and re.match(r'^(licen[cs]e|copying|notice|copyright)([.\-_]|$)', path.name, re.I))


def collect(name, version, directory, kind):
    target = output / kind / re.sub(r'[^a-zA-Z0-9._-]', '_', name + '@' + version)
    paths = documents(directory)
    target.mkdir(parents=True, exist_ok=True)
    for path in paths:
        shutil.copyfile(path, target / path.name)
    if not paths and name == '@vue/devtools-api':
        bundled = project_root / 'LICENSES/vue-devtools-MIT.txt'
        shutil.copyfile(bundled, target / 'LICENSE')
        paths = [target / 'LICENSE']
    return {'name': name, 'version': version, 'notices': [str((target / p.name).relative_to(output)) for p in paths]}


if args.only in ('all', 'frontend'):
    frontend = json.loads(subprocess.check_output(['pnpm', 'licenses', 'list', '--prod', '--json'], cwd=project_root / 'frontend', text=True))
    frontend_index = []
    for packages in frontend.values():
        for package in packages:
            for index, directory in enumerate(package['paths']):
                versions = package['versions']
                entry = collect(package['name'], versions[min(index, len(versions)-1)], Path(directory), 'frontend')
                entry.update({key: package[key] for key in ('license', 'author', 'homepage') if key in package})
                frontend_index.append(entry)
    (output / 'frontend-dependencies.json').write_text(json.dumps(frontend_index, ensure_ascii=False, indent=2) + '\n')

if args.only in ('all', 'go'):
    # 只收集本次编译实际用到的模块，不把开发机路径写进分发清单。
    raw = subprocess.check_output(['go', 'list', '-deps', '-json', './cmd/myanimego'], cwd=project_root / 'backend', text=True)
    decoder, position, modules = json.JSONDecoder(), 0, {}
    while position < len(raw):
        value, end = decoder.raw_decode(raw, position)
        position = end
        while position < len(raw) and raw[position].isspace():
            position += 1
        module = value.get('Module')
        if module and not module.get('Main'):
            module = module.get('Replace', module)
            modules[module['Path']] = module
    backend_index = [collect(name, module.get('Version', 'local'), Path(module['Dir']), 'go') for name, module in sorted(modules.items())]
    (output / 'go-dependencies.json').write_text(json.dumps(backend_index, ensure_ascii=False, indent=2) + '\n')

print(f'已收集依赖许可：{args.only}')
