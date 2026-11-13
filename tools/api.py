"""Executable API examples using only the Python standard library."""
import argparse
import json
import os
from pathlib import Path
import sys
import urllib.error
import urllib.request

p=argparse.ArgumentParser();p.add_argument('--base',default='http://127.0.0.1:8080');p.add_argument('command',choices=['health','list','search','detail','check','rollback','reload','install-local']);p.add_argument('--scraper');p.add_argument('--object',choices=['scene','performer'],default='scene');p.add_argument('--query');p.add_argument('--url');p.add_argument('--fragment',type=Path);p.add_argument('--operation',choices=['fragment','query_fragment']);p.add_argument('--version');a=p.parse_args()
paths={'health':('/api/health','GET'),'list':('/api/scrapers','GET'),'search':('/api/search','POST'),'detail':('/api/detail','POST'),'check':('/api/updates/check','POST'),'rollback':('/api/updates/rollback','POST'),'reload':('/api/reload','POST'),'install-local':('/api/install','POST')}
path,method=paths[a.command];body=None
if a.command in {'search','detail'}:
    if not a.scraper:p.error('--scraper is required')
    body={'scraper_id':a.scraper,'object':a.object}
    if a.query:body['query']=a.query
    if a.url:body['url']=a.url
    if a.fragment:body['fragment']=json.loads(a.fragment.read_text(encoding='utf-8'))
    if a.operation:body['operation']=a.operation
    if a.version:body['rules_version']=a.version
elif a.command=='rollback':body={'version':a.version or ''}
elif a.command=='install-local':body={'source':'local'}
headers={'Content-Type':'application/json'}
if os.environ.get('STASHSCRAPERS_TOKEN'):headers['Authorization']='Bearer '+os.environ['STASHSCRAPERS_TOKEN']
request=urllib.request.Request(a.base.rstrip('/')+path,data=json.dumps(body).encode() if body is not None else None,method=method,headers=headers)
try:
    with urllib.request.urlopen(request,timeout=300) as response:result=json.load(response)
except urllib.error.HTTPError as e:
    print(json.dumps(json.load(e),ensure_ascii=False,indent=2));sys.exit(1)
print(json.dumps(result,ensure_ascii=False,indent=2))
