"""Black-box checks of the VPS installer using controlled Docker/curl commands."""
import os
from pathlib import Path
import re
import subprocess
import tempfile

if os.environ.get('CONDA_DEFAULT_ENV')!='stashscrapers':raise SystemExit('Conda environment required')
project=Path.cwd().resolve()
output=project/'test-output';output.mkdir(exist_ok=True)
def linux_path(path):
    p=Path(path).resolve()
    return '/mnt/'+p.drive[0].lower()+p.as_posix().split(':',1)[1] if os.name=='nt' else str(p)
def bash(command,env):
    if os.name=='nt':
        args=['wsl','--distribution','Ubuntu-24.04','--exec','env',*[k+'='+v for k,v in env.items()],*command]
        return subprocess.run(args,text=True,capture_output=True,encoding='utf-8',timeout=45)
    return subprocess.run(command,text=True,capture_output=True,env={**os.environ,**env},timeout=45)
with tempfile.TemporaryDirectory(prefix='deployment-',dir=output) as temp:
    root=Path(temp).resolve();assert root.parent==output
    binaries=root/'bin';binaries.mkdir();trace=root/'docker.log';install=root/'installation'
    docker='''#!/bin/bash
set -eu
printf '%s|%s\\n' "$*" "${STASHSCRAPERS_IMAGE:-}" >> "$DOCKER_TRACE"
if [[ "$1" == inspect ]]; then printf 'sha256:oldimage\\n'; exit 0; fi
if [[ " $* " == *" ps -q scrapers "* ]]; then [[ "${MOCK_EXISTING:-0}" != 1 ]] || printf 'old-container\\n'; exit 0; fi
if [[ " $* " == *" up "* && "${MOCK_FAIL_UP:-0}" == 1 && "${STASHSCRAPERS_IMAGE:-}" != sha256:oldimage ]]; then exit 1; fi
exit 0
'''
    curl='''#!/bin/bash
set -eu
while [[ "$#" -gt 0 ]]; do
 if [[ "$1" == --output ]]; then cp "$FIXTURE_COMPOSE" "$2"; exit 0; fi
 shift
done
exit 1
'''
    for name,text in [('docker',docker),('curl',curl)]:
        file=binaries/name;file.write_text(text,encoding='utf-8',newline='\n');file.chmod(0o755)
    env={'PATH':linux_path(binaries)+':/usr/bin:/bin','DOCKER_TRACE':linux_path(trace),'FIXTURE_COMPOSE':linux_path(project/'deploy/compose.yml'),'STASHSCRAPERS_SOURCE_BASE':'https://controlled.fixture','INSTALL_DIR':linux_path(install)}
    command=['bash',linux_path(project/'deploy/install.sh')]
    syntax=bash(['bash','-n',linux_path(project/'deploy/install.sh')],env);assert syntax.returncode==0,syntax.stderr
    result=bash(command,env);assert result.returncode==0,result.stderr
    settings=(install/'.env').read_bytes();assert re.search(rb'STASHSCRAPERS_TOKEN=[0-9a-f]{64}',settings)
    assert b'BIND_ADDRESS=127.0.0.1' in settings and b'ghcr.io/nwfnyfgvfir/stashscrapers:latest' in settings
    result=bash(command,{**env,'VERSION':'v9.9.9','MOCK_EXISTING':'1'});assert result.returncode==0,result.stderr
    assert (install/'.env').read_bytes()==settings,'Repeated installation overwrote credentials or settings'
    result=bash(command,{**env,'VERSION':'invalid;command'});assert result.returncode!=0,'Unsafe image tag accepted'
    result=bash(command,{**env,'PORT':'0'});assert result.returncode!=0,'Invalid port accepted'
    result=bash(command,{**env,'MOCK_EXISTING':'1','MOCK_FAIL_UP':'1'});assert result.returncode!=0,'Failed health check was accepted'
    assert 'up -d --no-build --pull never --wait --wait-timeout 180|sha256:oldimage' in trace.read_text(),'Previous image was not restored'
    assert (install/'.env').read_bytes()==settings,'Rollback overwrote credentials'
print('VPS installer passed: fresh installation, credential preservation, input validation, failed-upgrade recovery')
