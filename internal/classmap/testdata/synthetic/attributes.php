<?php
namespace Attr;
#[Attribute(Attribute::TARGET_CLASS)]
class Marker {}
#[Marker, Other("class NotAttr")]
final class Marked {}
#[
  Multi
]
class MultiLine {}
# class NotHashComment
