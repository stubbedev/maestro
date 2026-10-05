<?php
require __DIR__.'/../../../.ref/composer/vendor/autoload.php';
$src=__DIR__.'/../../../.ref/composer/src/';
$it=new RecursiveIteratorIterator(new RecursiveDirectoryIterator($src));
$out=[];
foreach($it as $f){ if(substr($f,-4)!=='.php')continue;
  $cls=str_replace(['/','.php'],['\\',''],substr($f,strlen($src)));
  try{ if(!class_exists($cls)&&!interface_exists($cls)&&!trait_exists($cls)) continue; }catch(\Throwable $e){continue;}
  $r=new ReflectionClass($cls);
  $m=[];foreach($r->getMethods(ReflectionMethod::IS_PUBLIC) as $mm) $m[]=$mm->getName();
  $c=array_keys($r->getConstants());
  $out[$cls]=['kind'=>$r->isInterface()?'interface':($r->isTrait()?'trait':($r->isAbstract()?'abstract':'class')),'parent'=>$r->getParentClass()?$r->getParentClass()->getName():null,'interfaces'=>$r->getInterfaceNames(),'methods'=>$m,'consts'=>$c,'internal'=>(bool)preg_match('/@internal/',$r->getDocComment()?:'')];
}
ksort($out);
file_put_contents('apiindex.json',json_encode($out,JSON_PRETTY_PRINT));
echo count($out),"\n";
