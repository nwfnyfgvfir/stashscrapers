"""Build standalone Windows and Linux binaries plus checksums in Conda."""
import hashlib
import os
from pathlib import Path
import subprocess
import sys

if os.environ.get('CONDA_DEFAULT_ENV')!='stashscrapers':
    sys.exit('Use conda run -n stashscrapers python tools/build.py')
subprocess.run([sys.executable,'tools/package_source.py'],check=True)
Path('dist').mkdir(exist_ok=True)
targets=[('windows','amd64'),('linux','amd64'),('linux','arm64')]
for system,arch in targets:
    name=f'stashscrapers-{system}-{arch}'+('.exe' if system=='windows' else '')
    env=os.environ.copy();env.update(GOOS=system,GOARCH=arch,CGO_ENABLED='0')
    subprocess.run(['go','build','-trimpath','-ldflags=-s -w','-o',str(Path('dist')/name),'./cmd/stashscrapers'],env=env,check=True)
lines=[]
for file in sorted(Path('dist').iterdir()):
    if file.is_file() and file.name!='SHA256SUMS':
        digest=hashlib.sha256(file.read_bytes()).hexdigest();lines.append(f'{digest}  {file.name}')
Path('dist/SHA256SUMS').write_text('\n'.join(lines)+'\n')
print('\n'.join(f'{p.name}: {p.stat().st_size} bytes' for p in sorted(Path('dist').iterdir()) if p.is_file()))
