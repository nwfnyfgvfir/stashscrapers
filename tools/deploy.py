"""Pull a CI-published container, verify health, and restore the old image on failure.

Run on the deployment host. It requires an existing Compose deployment and Docker
permissions. It never changes config.json or rule snapshots.
"""
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request

if os.environ.get('CONDA_DEFAULT_ENV')!='stashscrapers':raise SystemExit('Conda environment required')
compose=Path(os.environ.get('DEPLOY_COMPOSE','compose.yml')).resolve()
image=os.environ.get('STASHSCRAPERS_IMAGE','')
if not image.startswith('ghcr.io/'):raise SystemExit('Set STASHSCRAPERS_IMAGE to the intended published GHCR image')
base=['docker','compose','-f',str(compose)]
def output(args,env=None):return subprocess.check_output(args,env=env,text=True).strip()
container=output(base+['ps','-q','scrapers'])
old=output(['docker','inspect','--format','{{.Image}}',container]) if container else ''
try:
    subprocess.run(base+['pull','scrapers'],check=True)
    subprocess.run(base+['up','-d','--no-build','scrapers'],check=True)
    deadline=time.monotonic()+120
    while True:
        try:
            with urllib.request.urlopen('http://127.0.0.1:8080/api/health',timeout=3) as response:health=json.load(response)
            if health['status']=='ok':break
        except Exception:
            if time.monotonic()>=deadline:raise RuntimeError('New engine did not become healthy')
            time.sleep(2)
    print('Deployed engine '+health['engine'])
except Exception:
    if old:
        env=os.environ.copy();env['STASHSCRAPERS_IMAGE']=old
        subprocess.run(base+['up','-d','--no-build','scrapers'],env=env,check=True)
        print('Restored previous engine image after failed upgrade')
    raise
