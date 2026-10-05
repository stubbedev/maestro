import json,os,re,sys,glob,collections
api=json.load(open('apiindex.json'))
methods_by_name=collections.defaultdict(set)
for c,i in api.items():
    for m in i['methods']: methods_by_name[m].add(c)
NS=('Composer\\','Symfony\\Component\\Console','Symfony\\Component\\Process','Symfony\\Component\\Filesystem','Symfony\\Component\\Finder','React\\Promise','Seld\\','Psr\\Log','JsonSchema\\')
SCOPE={'laravel__framework':['src/Illuminate/Foundation/ComposerScripts.php'],
       'symfony__runtime':['Internal'],'phpro__grumphp':['src/Composer'],'php-http__discovery':['src/Composer'],
       'ocramius__package-versions':['src']}
SKIP=re.compile(r'/(tests?|Tests?|vendor|fixtures|Fixtures|stubs)/')
rows=[];pk=[]
for d in sorted(glob.glob('*@*')):
    root=os.path.dirname(sorted(glob.glob(d+'/*/composer.json'))[0])
    cj=json.load(open(root+'/composer.json'))
    key=d.split('@')[0]
    name=cj.get('name',key.replace('__','/'))
    ver=d.split('@')[1]
    scope=SCOPE.get(key,['.'])
    files=[]
    for s in scope:
        p=os.path.join(root,s)
        if os.path.isfile(p): files.append(p)
        else:
            for dp,_,fs in os.walk(p):
                for f in fs:
                    fp=os.path.join(dp,f)
                    if f.endswith('.php') and not SKIP.search(fp[len(root):]): files.append(fp)
    syms=collections.Counter(); used=0
    for fp in files:
        t=open(fp,errors='replace').read()
        t2=re.sub(r'(?s)/\*.*?\*/','',t); t2=re.sub(r'(?m)//.*$|^\s*#.*$','',t2)
        alias={}
        for m in re.finditer(r'(?m)^use\s+([\w\\]+)(?:\s+as\s+(\w+))?\s*;',t2):
            fq=m.group(1); alias[m.group(2) or fq.split('\\')[-1]]=fq
        for m in re.finditer(r'(?m)^use\s+([\w\\]+)\\\{([^}]*)\}',t2):
            for part in m.group(2).split(','):
                part=part.strip()
                if not part: continue
                pp=part.split(' as ')
                fq=m.group(1)+'\\'+pp[0].strip(); alias[(pp[1] if len(pp)>1 else pp[0]).strip().split('\\')[-1]]=fq
        hit=False
        for a,fq in alias.items():
            if not fq.startswith(NS): continue
            hit=True
            syms[('class',fq)]+=1
            for m in re.finditer(r'(?<![\w\\])'+re.escape(a)+r'::(\w+)(\s*\()?',t2):
                syms[('static' if m.group(2) else 'const',fq+'::'+m.group(1))]+=1
            if re.search(r'\bextends\s+'+re.escape(a)+r'\b',t2): syms[('extends',fq)]+=1
            if re.search(r'\bimplements\s+[^{]*\b'+re.escape(a)+r'\b',t2): syms[('implements',fq)]+=1
            if re.search(r'\bnew\s+'+re.escape(a)+r'\b',t2): syms[('new',fq)]+=1
        for m in re.finditer(r'(?<![\w\\])\\((?:Composer|Symfony\\Component\\(?:Console|Process|Filesystem|Finder)|React\\Promise|Seld)\\[\w\\]+)(::\w+)?',t2):
            hit=True; fq=m.group(1)
            syms[('class',fq)]+=1
            if m.group(2): syms[('const' ,fq+m.group(2))]+=1
        if hit:
            used+=1
            for m in re.finditer(r'->(\w+)\s*\(',t2):
                if m.group(1) in methods_by_name: syms[('method',m.group(1))]+=1
    for (k,s),n in sorted(syms.items()): rows.append((name+'@'+ver,k,s,n))
    pk.append((name,ver,str(cj.get('extra',{}).get('class','')),cj.get('require',{}).get('composer-plugin-api',''),len(files),used))
with open('symbols.tsv','w') as f:
    f.write('package\tkind\tsymbol\tcount\n')
    for r in rows: f.write('\t'.join(map(str,r))+'\n')
with open('packages.tsv','w') as f:
    f.write('package\tversion\tplugin_class\tcomposer_plugin_api\tphp_files_scanned\tfiles_using_composer_api\n')
    for r in pk: f.write('\t'.join(map(str,r))+'\n')
print(len(rows))
