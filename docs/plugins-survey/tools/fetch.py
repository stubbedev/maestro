import json,sys,urllib.request,os,zipfile,io,re
pkgs=sys.argv[1:]
for spec in pkgs:
    name,_,pref=spec.partition('@')
    try:
        d=json.load(urllib.request.urlopen(f'https://repo.packagist.org/p2/{name}.json'))
    except Exception as e:
        print(name,'ERR',e);continue
    vers=d['packages'][name]
    # minified: first entry is latest; expand
    full=[];cur={}
    for v in vers:
        cur={**cur,**v}; cur={k:x for k,x in cur.items() if x!='__unset'}; full.append(dict(cur))
    cands=[v for v in full if re.match(r'^v?\d+(\.\d+)*$',v['version']) and (not pref or v['version'].lstrip('v').startswith(pref))]
    if not cands: print(name,'no stable');continue
    v=cands[0]
    dest=f"{name.replace('/','__')}@{v['version']}"
    if not os.path.exists(dest):
        z=zipfile.ZipFile(io.BytesIO(urllib.request.urlopen(v['dist']['url']).read()))
        z.extractall(dest)
    print(name,v['version'],dest)
