"""Verify original fixture hashes and reproduce the generated engine byte-for-byte."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

if os.environ.get('CONDA_DEFAULT_ENV')!='stashscrapers':raise SystemExit('Conda environment required')
p=argparse.ArgumentParser();p.add_argument('--source',type=Path);a=p.parse_args()
lock=json.loads(Path('upstream/community-lock.json').read_text())
for name,wanted in lock['fixtures_sha256'].items():
    file=Path('internal/service/testdata/community')/name
    assert hashlib.sha256(file.read_bytes()).hexdigest()==wanted, 'Original fixture was modified: '+name
print(f'Original community files verified: {len(lock["fixtures_sha256"])}')
if a.source:
    files=sorted(Path('internal/engine').glob('upstream_*.go'))
    before={file.name:hashlib.sha256(file.read_bytes()).hexdigest() for file in files}
    sha=json.loads(Path('upstream/engine-lock.json').read_text())['commit']
    subprocess.run([sys.executable,'tools/sync_engine.py','--source',str(a.source),'--sha',sha],check=True)
    after={file.name:hashlib.sha256(file.read_bytes()).hexdigest() for file in files}
    assert before==after,'Engine extraction is not byte-for-byte reproducible'
    print(f'Engine extraction reproduced byte-for-byte: {len(files)} files')
