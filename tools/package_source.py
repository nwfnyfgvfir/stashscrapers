"""Create the network source offer without credentials, binaries or snapshots."""
import io
import gzip
from pathlib import Path
import tarfile
out=Path('internal/web/source.tar.gz')
skip={'.git','.upstream','data','bin','dist','test-output','__pycache__'}
with out.open('wb') as raw, gzip.GzipFile(filename='',fileobj=raw,mode='wb',mtime=0) as compressed, tarfile.open(fileobj=compressed,mode='w') as archive:
    for file in sorted(Path('.').rglob('*')):
        if file.name=='.env' or file.name.startswith('.env.') and file.name!='.env.example':
            continue
        if not file.is_file() or any(part in skip for part in file.parts) or file==out or file.suffix in {'.log','.exe'}:
            continue
        info=archive.gettarinfo(str(file),arcname='stashscrapers/'+file.as_posix())
        info.mtime=0;info.uid=0;info.gid=0;info.uname='';info.gname=''
        with file.open('rb') as data:archive.addfile(info,data)
print(f'Source archive: {out.stat().st_size} bytes')
