<?php
$x = <<<A
{${<<<B
class NotInner
B}}
class NotOuter
A;
class AfterNested {}
