<?php
namespace Hd;
$a = <<<EOT
class NotA {}
EOT;
$b = <<<"EOT"
  class NotB {$x} ${y}
  EOT;
$c = <<<'EOT'
class NotC
EOT;
$d = <<< EOT
class NotD
EOT
;
$e = <<<	EOTX
	class NotE
	EOTX . 'x';
$f = <<<EOT
EOTclass NotF
EOT;
class RealA {}
