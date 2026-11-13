"""Cross-compile tests inside Conda, then execute the standalone test binary in WSL."""
import os
from pathlib import Path
import subprocess
if os.environ.get('CONDA_DEFAULT_ENV')!='stashscrapers':raise SystemExit('Conda environment required')
Path('test-output').mkdir(exist_ok=True)
env=os.environ.copy();env.update(GOOS='linux',GOARCH='amd64',CGO_ENABLED='0')
subprocess.run(['go','test','-c','-o','test-output/service-linux.test','./internal/service'],env=env,check=True)
root=Path.cwd();drive=root.drive[0].lower();linux='/mnt/'+drive+'/'+root.as_posix().split(':',1)[1].lstrip('/')+'/test-output/service-linux.test'
subprocess.run(['wsl','--distribution','Ubuntu-24.04','--exec',linux,'-test.v'],check=True)
