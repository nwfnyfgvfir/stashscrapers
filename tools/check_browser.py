"""Exercise the service's optional CDP runtime with an existing headless browser.
Uses an isolated temporary profile and never opens or modifies a user profile.
"""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time

if os.environ.get('CONDA_DEFAULT_ENV')!='stashscrapers':raise SystemExit('Conda environment required')
browser=os.environ.get('TEST_BROWSER')
if not browser:
    candidates=[r'C:\Program Files\Google\Chrome\Application\chrome.exe',r'C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe']
    browser=next((p for p in candidates if Path(p).is_file()),None) or shutil.which('chromium') or shutil.which('google-chrome')
if not browser:raise SystemExit('Set TEST_BROWSER to an installed Chromium executable')
with tempfile.TemporaryDirectory(prefix='stashscrapers-cdp-') as profile:
    flags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0
    proc=subprocess.Popen([browser,'--headless=new','--remote-debugging-port=0','--no-first-run','--no-default-browser-check','--disable-extensions','--user-data-dir='+profile,'about:blank'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,creationflags=flags)
    try:
        active=Path(profile)/'DevToolsActivePort';deadline=time.monotonic()+20
        while not active.exists():
            if time.monotonic()>deadline or proc.poll() is not None:raise RuntimeError('Headless CDP browser failed to start')
            time.sleep(.1)
        port=active.read_text().splitlines()[0];env=os.environ.copy();env['TEST_CDP']='http://127.0.0.1:'+port
        subprocess.run(['go','test','./internal/service','-run','TestOptionalCDPDynamicDOM','-count=1','-v'],check=True,env=env)
    finally:
        proc.terminate()
        try:proc.wait(timeout=10)
        except subprocess.TimeoutExpired:proc.kill();proc.wait()
