"""Measure standalone Windows process startup and idle memory without touching live data."""
import argparse
import ctypes
from ctypes import wintypes
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request

p=argparse.ArgumentParser();p.add_argument('--binary',type=Path,default=Path('dist/stashscrapers-windows-amd64.exe'));p.add_argument('--data',type=Path,default=Path('data'));a=p.parse_args()
if os.name!='nt':raise SystemExit('This measurement uses Windows process working set counters')
class Counters(ctypes.Structure):
    _fields_=[('cb',wintypes.DWORD),('PageFaultCount',wintypes.DWORD),('PeakWorkingSetSize',ctypes.c_size_t),('WorkingSetSize',ctypes.c_size_t),('QuotaPeakPagedPoolUsage',ctypes.c_size_t),('QuotaPagedPoolUsage',ctypes.c_size_t),('QuotaPeakNonPagedPoolUsage',ctypes.c_size_t),('QuotaNonPagedPoolUsage',ctypes.c_size_t),('PagefileUsage',ctypes.c_size_t),('PeakPagefileUsage',ctypes.c_size_t),('PrivateUsage',ctypes.c_size_t)]
with tempfile.TemporaryDirectory(prefix='stashscrapers-measure-') as temp:
    root=Path(temp)/'data';shutil.copytree(a.data,root,ignore=shutil.ignore_patterns('staging','runtimes'))
    config=json.loads((root/'config.json').read_text());config['update']['enabled']=False;(root/'config.json').write_text(json.dumps(config))
    with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
    log=Path(temp)/'service.log';started=time.perf_counter()
    with log.open('wb') as stream:
        proc=subprocess.Popen([str(a.binary.resolve()),'--data',str(root),'--listen',f'127.0.0.1:{port}'],stdout=stream,stderr=stream,creationflags=subprocess.CREATE_NO_WINDOW)
        try:
            while True:
                try:
                    with urllib.request.urlopen(f'http://127.0.0.1:{port}/api/health',timeout=.2) as r:health=json.load(r)
                    if health['ready']:break
                except Exception:pass
                if time.perf_counter()-started>30:raise RuntimeError('Startup failed')
                time.sleep(.01)
            elapsed=(time.perf_counter()-started)*1000;time.sleep(3)
            kernel=ctypes.WinDLL('kernel32',use_last_error=True);psapi=ctypes.WinDLL('psapi',use_last_error=True);kernel.OpenProcess.restype=wintypes.HANDLE;kernel.OpenProcess.argtypes=[wintypes.DWORD,wintypes.BOOL,wintypes.DWORD];kernel.CloseHandle.argtypes=[wintypes.HANDLE];psapi.GetProcessMemoryInfo.argtypes=[wintypes.HANDLE,ctypes.POINTER(Counters),wintypes.DWORD]
            handle=kernel.OpenProcess(0x410,False,proc.pid);memory=Counters();memory.cb=ctypes.sizeof(memory)
            if not psapi.GetProcessMemoryInfo(handle,ctypes.byref(memory),memory.cb):raise ctypes.WinError(ctypes.get_last_error())
            kernel.CloseHandle(handle)
            result={'platform':'Windows amd64','rules_version':health['rules_version'],'engine':health['engine'],'health_ready_ms':round(elapsed,1),'idle_after_seconds':3,'working_set_bytes':memory.WorkingSetSize,'private_bytes':memory.PrivateUsage,'binary_bytes':a.binary.stat().st_size,'docker_image_size':None,'docker_note':'Docker is not installed on this workstation'}
        finally:proc.terminate();proc.wait(timeout=10)
    result['service_log']=log.read_text().strip()
Path('test-output').mkdir(exist_ok=True);Path('test-output/metrics.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2))
