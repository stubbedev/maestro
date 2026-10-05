// Ports php_var_serialize from ext/standard/var.c for the values of this
// package.

package php

import (
	"fmt"
	"strconv"
)

// Serialize returns serialize($v). Floats use serialize_precision -1, as
// Composer runs with.
func Serialize(v any) string { return string(AppendSerialize(nil, v)) }

// AppendSerialize appends serialize($v) to dst.
func AppendSerialize(dst []byte, v any) []byte {
	switch v := v.(type) {
	case nil:
		return append(dst, "N;"...)
	case bool:
		if v {
			return append(dst, "b:1;"...)
		}

		return append(dst, "b:0;"...)
	case int64:
		return append(strconv.AppendInt(append(dst, "i:"...), v, 10), ';')
	case int:
		return append(strconv.AppendInt(append(dst, "i:"...), int64(v), 10), ';')
	case float64:
		return append(appendDouble(append(dst, "d:"...), v, -1, false), ';')
	case string:
		return appendSerializeString(dst, v)
	case *Array:
		dst = append(strconv.AppendInt(append(dst, "a:"...), int64(v.Len()), 10), ":{"...)
		for k, x := range v.All() {
			if k.kind == kindInt {
				dst = append(strconv.AppendInt(append(dst, "i:"...), k.i, 10), ';')
			} else {
				dst = appendSerializeString(dst, k.s)
			}
			dst = AppendSerialize(dst, x)
		}

		return append(dst, '}')
	case *Object:
		dst = append(strconv.AppendInt(append(dst, `O:8:"stdClass":`...), int64(v.Len()), 10), ":{"...)
		for name, x := range v.All() {
			dst = appendSerializeString(dst, name)
			dst = AppendSerialize(dst, x)
		}

		return append(dst, '}')
	}

	panic(fmt.Sprintf("php: unsupported value type %T", v))
}

func appendSerializeString(dst []byte, s string) []byte {
	dst = strconv.AppendInt(append(dst, "s:"...), int64(len(s)), 10)
	dst = append(append(dst, ":\""...), s...)

	return append(dst, "\";"...)
}
