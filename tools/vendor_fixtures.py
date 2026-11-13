"""Copy untouched community rule fixtures and their license from a pinned tree."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil

p=argparse.ArgumentParser();p.add_argument('--source',type=Path,required=True);a=p.parse_args()
dest=Path('internal/service/testdata');dest.mkdir(parents=True,exist_ok=True)
files=['AdultEmpire.yml','1pondo.yml','Boobpedia.yml','Anime-DB.yml','RealjamVR/RealjamVR.yml',
       'RealjamVR/RealJamVR.py','py_common/util.py','py_common/types.py','py_common/log.py','py_common/__init__.py']
hashes={}
for file in files:
    src=a.source/'scrapers'/file
    if not src.exists() and file.endswith('RealJamVR.py'):
        src=next((a.source/'scrapers/RealjamVR').glob('*.py'))
    if not src.exists() and file.endswith('__init__.py'):
        continue
    output=dest/'community'/file;output.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(src,output)
    hashes[file]=hashlib.sha256(src.read_bytes()).hexdigest()
Path('upstream').mkdir(exist_ok=True)
shutil.copyfile(a.source/'LICENSE','upstream/COMMUNITY-LICENSE')
Path('upstream/community-lock.json').write_text(json.dumps({'repository':'https://github.com/stashapp/CommunityScrapers',
    'commit':'da55cf6ee5ff12b4b6bc33b05e306d1fbe550d6b','license':'AGPL-3.0','fixtures_sha256':hashes},indent=2)+'\n')
