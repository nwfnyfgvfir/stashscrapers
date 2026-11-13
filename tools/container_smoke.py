"""Verify the built container starts with a writable named data volume."""
import json
import subprocess
import time
import urllib.request
import uuid
import urllib.error
name='stashscrapers-smoke-'+uuid.uuid4().hex[:8];volume=name+'-data'
try:
    subprocess.run(['docker','volume','create',volume],check=True)
    subprocess.run(['docker','run','-d','--name',name,'-v',volume+':/data','-e','STASHSCRAPERS_TOKEN=container-smoke-fixture-token','-p','127.0.0.1::8080','stashscrapers:ci'],check=True)
    port=subprocess.check_output(['docker','port',name,'8080/tcp'],text=True).strip().split(':')[-1]
    deadline=time.monotonic()+45
    while True:
        try:
            with urllib.request.urlopen('http://127.0.0.1:'+port+'/api/health',timeout=2) as r:health=json.load(r)
            assert health['status']=='ok';break
        except Exception:
            if time.monotonic()>deadline:raise
            time.sleep(.25)
    subprocess.run(['docker','exec',name,'/stashscrapers','--healthcheck'],check=True)
    user=subprocess.check_output(['docker','inspect',name,'--format','{{.Config.User}}'],text=True).strip()
    assert user not in {'','0','root','0:0'},'Container is running as root'
    base='http://127.0.0.1:'+port
    try:urllib.request.urlopen(base+'/api/config');raise AssertionError('Missing service token was accepted')
    except urllib.error.HTTPError as error:assert error.code==401
    headers={'Authorization':'Bearer container-smoke-fixture-token','Content-Type':'application/json'}
    with urllib.request.urlopen(urllib.request.Request(base+'/api/config',headers=headers)) as response:settings=json.load(response)
    settings['timeout_seconds']=42;settings['update']['enabled']=False
    request=urllib.request.Request(base+'/api/config',headers=headers,data=json.dumps(settings).encode(),method='PUT')
    with urllib.request.urlopen(request) as response:assert response.status==200
    subprocess.run(['docker','restart',name],check=True)
    deadline=time.monotonic()+45
    while True:
        try:
            with urllib.request.urlopen(urllib.request.Request(base+'/api/config',headers=headers),timeout=2) as response:restored=json.load(response)
            assert restored['timeout_seconds']==42,'Data volume settings were lost after restart';break
        except (OSError,urllib.error.URLError):
            if time.monotonic()>deadline:raise
            time.sleep(.25)
    print('Non-root container, health CLI, authentication and persistent-volume restart passed')
    print(json.dumps(health));print(subprocess.check_output(['docker','image','inspect','stashscrapers:ci','--format','{{.Size}}'],text=True).strip()+' image bytes')
finally:
    subprocess.run(['docker','rm','-f',name],check=False)
    subprocess.run(['docker','volume','rm',volume],check=False)
