"""Publish only the fully tested CI workspace; requires repository write token."""
import json
import os
from pathlib import Path
import re
import subprocess
import urllib.request

if os.environ.get('CONDA_DEFAULT_ENV')!='stashscrapers':raise SystemExit('Conda environment required')
repo=os.environ['GITHUB_REPOSITORY'];token=os.environ['GITHUB_TOKEN'];sha=os.environ['ENGINE_SHA'];branch=os.environ.get('GITHUB_REF_NAME','')
if not re.fullmatch(r'[0-9a-f]{40}',sha) or not re.fullmatch(r'[A-Za-z0-9_./-]+',branch):raise SystemExit('Invalid release identity')
def run(*args):subprocess.run(list(args),check=True)
run('git','config','user.name','stashscrapers-engine-bot');run('git','config','user.email','engine-bot@users.noreply.github.com')
run('git','add','internal/engine','internal/web/source.tar.gz','upstream','go.mod','go.sum')
run('git','commit','-m','chore: validate standalone scraper engine '+sha[:12])
run('git','push','origin','HEAD:refs/heads/'+branch)
tag='engine-'+sha[:12];run('git','tag',tag);run('git','push','origin','refs/tags/'+tag)
if os.environ.get('GITHUB_OUTPUT'):
    with open(os.environ['GITHUB_OUTPUT'],'a',encoding='utf-8') as file:file.write('tag='+tag+'\n')
headers={'Authorization':'Bearer '+token,'Accept':'application/vnd.github+json','User-Agent':'stashscrapers-engine-release'}
body={'tag_name':tag,'name':'Standalone scraper engine '+sha[:12],'body':'Engine extraction, pinned community fixtures, standalone Python HTTPS integration, race checks and binary builds passed in CI. Community rules continue to update independently.','draft':False,'prerelease':False}
req=urllib.request.Request('https://api.github.com/repos/'+repo+'/releases',data=json.dumps(body).encode(),headers={**headers,'Content-Type':'application/json'},method='POST')
with urllib.request.urlopen(req,timeout=60) as response:release=json.load(response)
upload=release['upload_url'].split('{')[0]
for file in sorted(Path('dist').iterdir()):
    if not file.is_file():continue
    req=urllib.request.Request(upload+'?name='+file.name,data=file.read_bytes(),headers={**headers,'Content-Type':'application/octet-stream'},method='POST')
    with urllib.request.urlopen(req,timeout=180) as response:json.load(response)
print('Published '+release['html_url'])
