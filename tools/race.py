"""Enable cgo explicitly for Conda Go variants and run the race detector."""
import os
import subprocess
import sys
if os.environ.get('CONDA_DEFAULT_ENV')!='stashscrapers':raise SystemExit('Conda environment required')
env=os.environ.copy();env['CGO_ENABLED']='1'
subprocess.run(['go','test','-race','./...','-count=1'],check=True,env=env)
