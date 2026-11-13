"""Validate a release tag and pass pinned image coordinates to GitHub Actions."""
import json
import os
import re
import subprocess

repo=os.environ['GITHUB_REPOSITORY']
tag=os.environ['RELEASE_TAG']
if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+',repo):raise SystemExit('Invalid repository identity')
stable=bool(re.fullmatch(r'v\d+\.\d+\.\d+',tag))
engine=bool(re.fullmatch(r'engine-[0-9a-f]{7,40}',tag))
if not stable and not engine and not re.fullmatch(r'v\d+\.\d+\.\d+-[A-Za-z0-9][A-Za-z0-9.-]*',tag):raise SystemExit('Use vMAJOR.MINOR.PATCH, a prerelease, or engine-SHA')
revision=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
expected=subprocess.check_output(['git','rev-parse','refs/tags/'+tag+'^{commit}'],text=True).strip()
if revision!=expected:raise SystemExit('Checkout does not match the requested release tag')
image='ghcr.io/'+repo.lower()
tags=[image+':'+tag,image+':sha-'+revision[:12]]
if stable or engine:tags.append(image+':latest')
values={'image':image,'version':tag,'revision':revision,'tags':'\n'.join(tags)}
if os.environ.get('GITHUB_OUTPUT'):
    with open(os.environ['GITHUB_OUTPUT'],'a',encoding='utf-8') as file:
        for key,value in values.items():file.write(key+'<<OUTPUT_VALUE\n'+value+'\nOUTPUT_VALUE\n')
print(json.dumps(values,indent=2))
