"""Run project checks from its Conda environment; no global tool fallback."""
import os
from pathlib import Path
import subprocess
import sys
import py_compile

if os.environ.get('CONDA_DEFAULT_ENV') != 'stashscrapers':
    sys.exit('Run with conda run -n stashscrapers python tools/check.py')
env=os.environ.copy();env['TEST_PYTHON']=sys.executable
for script in Path('tools').glob('*.py'):py_compile.compile(str(script),doraise=True)
subprocess.run([sys.executable,'tools/verify_provenance.py'],check=True,env=env)
if '--full' in sys.argv:env['TEST_SCRIPT_RUNTIME']='1'
subprocess.run(['go','test','./...','-count=1','-v'],check=True,env=env)
subprocess.run(['go','vet','./...'],check=True,env=env)
