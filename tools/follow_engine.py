"""Daily engine upgrade gate. Failed extraction/tests never alter a deployment."""
import argparse
import io
import json
import os
from pathlib import Path
import re
import subprocess
import urllib.request
import zipfile
from sync_engine import extract

if os.environ.get('CONDA_DEFAULT_ENV')!='stashscrapers':
    raise SystemExit('Run inside the stashscrapers Conda environment')
p=argparse.ArgumentParser();p.add_argument('--ref',default='develop');a=p.parse_args()
headers={'User-Agent':'stashscrapers-engine-upgrade'}
token=os.environ.get('GITHUB_TOKEN')
if token:headers['Authorization']='Bearer '+token
def read(url):
    with urllib.request.urlopen(urllib.request.Request(url,headers=headers),timeout=120) as r:return r.read(128*1024*1024)
commit=json.loads(read('https://api.github.com/repos/stashapp/stash/commits/'+urllib.parse.quote(a.ref,safe='')))['sha']
if not re.fullmatch('[0-9a-f]{40}',commit):raise RuntimeError('Invalid upstream SHA')
current=json.loads(Path('upstream/engine-lock.json').read_text())['commit']
changed=commit!=current
if os.environ.get('GITHUB_OUTPUT'):
    with open(os.environ['GITHUB_OUTPUT'],'a') as f:f.write(f'changed={str(changed).lower()}\nsha={commit}\n')
if not changed:print('Engine already matches upstream');raise SystemExit(0)
root=Path('.upstream');root.mkdir(exist_ok=True)
with zipfile.ZipFile(io.BytesIO(read('https://codeload.github.com/stashapp/stash/zip/'+commit))) as z:
    for info in z.infolist():
        if '\\' in info.filename or ':' in info.filename or '..' in Path(info.filename).parts or Path(info.filename).is_absolute():raise RuntimeError('Unsafe upstream archive path')
    z.extractall(root)
checkout=root/('stash-'+commit)
extract(checkout,Path('internal/engine'),commit)
# Follow only dependencies used by the extraction, not the full Stash graph.
mod=(checkout/'go.mod').read_text()
names=['github.com/antchfx/htmlquery','github.com/antchfx/xpath','github.com/dop251/goja','github.com/tidwall/gjson','github.com/chromedp/chromedp','github.com/chromedp/cdproto','golang.org/x/net','gopkg.in/yaml.v2']
deps=[]
for name in names:
    match=re.search(r'^\s*'+re.escape(name)+r'\s+(\S+)',mod,re.M)
    if match:deps.append(name+'@'+match[1])
subprocess.run(['go','get',*deps],check=True)
subprocess.run(['go','mod','tidy'],check=True)
print(f'Extracted {commit}; compatibility checks are required before release')
