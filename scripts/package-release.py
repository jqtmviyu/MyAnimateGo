#!/usr/bin/env python3
"""将当前架构的二进制、文档与许可证打包为 Release 附件。"""
import argparse
from pathlib import Path
import re
import tarfile
import zipfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--source', type=Path, required=True)
parser.add_argument('--target', required=True)
parser.add_argument('--version', required=True)
parser.add_argument('--output', type=Path, required=True)
args = parser.parse_args()
version = re.sub(r'[^A-Za-z0-9._-]+', '-', args.version)
name = f'myanimego-{version}-{args.target}'
program = 'myanimego.exe' if args.target.startswith('windows-') else 'myanimego'
for required in (program, 'README.md', 'LICENSE', 'LICENSES'):
    if not (args.source / required).exists():
        parser.error(f'发布包缺少 {required}')
args.output.mkdir(parents=True, exist_ok=True)
if args.target.startswith('windows-'):
    target = args.output / f'{name}.zip'
    with zipfile.ZipFile(target, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
        for path in sorted(args.source.rglob('*')):
            if path.is_file():
                archive.write(path, Path(name) / path.relative_to(args.source))
else:
    target = args.output / f'{name}.tar.gz'
    with tarfile.open(target, 'w:gz') as archive:
        archive.add(args.source, arcname=name)
print(target)
