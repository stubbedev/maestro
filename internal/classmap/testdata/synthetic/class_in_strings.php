<?php
$a = 'class NotSingle {}';
$b = "class NotDouble {$c} {}";
$d = `class NotBacktick`;
$e = "\"class NotEscaped";
$f = 'it\'s class NotEscaped2';
// class NotLineComment
# class NotHash
/* class NotBlock */
/** class NotDoc */
class RealOne {}
echo Foo::class, $x->class, $class;
