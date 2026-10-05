<?php
namespace T\I;
interface Iface extends \Countable {}
trait Tr { use Other; }
abstract class Abs implements Iface {}
final class Fin {}
readonly class Ro {}
final readonly class FinRo {}
abstract readonly class AbsRo {}
