"""Optional LIVE tests; output is separate from offline compatibility results."""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import time
import urllib.request
import urllib.error

p=argparse.ArgumentParser();p.add_argument('--base',default='http://127.0.0.1:8080');a=p.parse_args()
cases=[{'scraper_id':'1pondo','object':'scene','url':'https://en.1pondo.tv/movies/010124_001/'},
       {'scraper_id':'Boobpedia','object':'performer','query':'Fixture Person'},
       {'scraper_id':'AdultEmpire','object':'scene','query':'fixture query'}]
out={'type':'live_target_smoke','at':datetime.now(timezone.utc).isoformat(),'cases':[]}
for case in cases:
    start=time.monotonic();path='/api/search' if 'query' in case else '/api/detail'
    request=urllib.request.Request(a.base+path,data=json.dumps(case).encode(),method='POST',headers={'Content-Type':'application/json'})
    try:
        with urllib.request.urlopen(request,timeout=40) as response:status=response.status;body=json.load(response)
    except urllib.error.HTTPError as e:status=e.code;body=json.load(e)
    except Exception as e:status=0;body={'error':{'code':type(e).__name__,'message':'service transport failed'}}
    # Do not publish target metadata, signed image URLs or credentials in logs.
    out['cases'].append({'scraper_id':case['scraper_id'],'status':status,'elapsed_seconds':round(time.monotonic()-start,3),'error':body.get('error'),'result_count':len(body.get('results',[])),'rules_version':body.get('rules_version')})
Path('test-output').mkdir(exist_ok=True);Path('test-output/online-smoke.json').write_text(json.dumps(out,indent=2)+'\n')
print(json.dumps(out,indent=2))
