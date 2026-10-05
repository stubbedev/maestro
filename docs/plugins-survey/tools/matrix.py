import csv,collections,json
api=json.load(open('apiindex.json'))
BUNDLED=('Composer\\Semver','Composer\\Pcre','Composer\\XdebugHandler','Composer\\CaBundle','Composer\\Spdx','Composer\\ClassMapGenerator','Composer\\MetadataMinifier','Composer\\Autoload\\ClassLoader','Composer\\InstalledVersions','Symfony\\','React\\','Seld\\','Psr\\','JsonSchema\\')
def valid(cls):
    return cls in api or (cls.startswith(BUNDLED) and cls.count('\\')>=2) or cls in ('Composer\\InstalledVersions',)
r=list(csv.DictReader(open('symbols.tsv'),delimiter='\t'))
agg=collections.defaultdict(set)
for x in r:
    k=x['kind'];s=x['symbol']
    if k!='method':
        cls=s.split('::')[0]
        if not valid(cls): continue
        if k in('const','static') and s.endswith('::class'): k='class'; s=cls
    agg[(k,s)].add(x['package'])
def short(p):
    n,v=p.split('@')
    return n+'@'+v.lstrip('v').split('.')[0] if n=='cweagans/composer-patches' else n
order={'class':0,'extends':1,'implements':2,'new':3,'const':4,'static':5,'method':6}
with open('usage-matrix.tsv','w') as f:
    f.write('kind\tsymbol\tpackages\tpackage_list\n')
    for (k,s),ps in sorted(agg.items(),key=lambda a:(order[a[0][0]],-len(a[1]),a[0][1])):
        f.write(f"{k}\t{s}\t{len({p.split('@')[0] for p in ps})}\t{','.join(sorted({short(p) for p in ps}))}\n")
print(len(agg))
