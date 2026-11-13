"""Check the delivered local service and save a compact, non-secret record."""
import io
import json
from pathlib import Path
import tarfile
import time
import urllib.error
import urllib.request

base='http://127.0.0.1:18080'
deadline=time.monotonic()+60
def get(path):
    with urllib.request.urlopen(base+path,timeout=20) as response:return json.load(response)
while True:
    try:
        health=get('/api/health')
        if health['ready']:break
    except Exception:
        if time.monotonic()>deadline:raise
        time.sleep(.25)
catalog=get('/api/scrapers')['scrapers'];updates=get('/api/updates');contract=get('/api/openapi.json')
assert len(catalog)==707 and len(contract['paths'])>=13
assert updates['schedule']['enabled'] and updates['schedule']['interval_hours']==24
assert updates['state']['active']=='da55cf6ee5ff12b4b6bc33b05e306d1fbe550d6b'
assert updates['state']['next_check']=='2026-11-14T11:19:33.9293743+08:00','Restart changed the persisted schedule'
with urllib.request.urlopen(base+'/api/source') as response:source=response.read()
with tarfile.open(fileobj=io.BytesIO(source),mode='r:gz') as archive:
    names=archive.getnames()
    assert 'stashscrapers/README.md' in names and 'stashscrapers/LICENSE' in names
    assert not any('/data/' in name or '/.upstream/' in name for name in names)
body={'scraper_id':'1pondo','object':'scene','query':'no name action exists'}
req=urllib.request.Request(base+'/api/search',data=json.dumps(body).encode(),headers={'Content-Type':'application/json'},method='POST')
try:urllib.request.urlopen(req);raise AssertionError('URL-only rule allowed name search')
except urllib.error.HTTPError as error:
    assert error.code==422 and json.load(error)['error']['code']=='unsupported'
result={'health':health,'definitions':len(catalog),'prerequisites_satisfied':sum(entry['state']=='available' for entry in catalog),'offline_verified_definitions':sum(entry['verification']=='offline_fixture' for entry in catalog),'openapi_paths':len(contract['paths']),'source_offer_bytes':len(source),'next_check':updates['state']['next_check'],'daily_updates_enabled':True,'url_only_capability_validation':'passed'}
Path('test-output/final-verification.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2))
