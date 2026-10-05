// Ports php_var_export_ex from ext/standard/var.c.

package php

import (
	"math"
	"strconv"
)

// VarExport returns var_export($v, true).
func VarExport(v any) string {
	return string(AppendVarExport(nil, v))
}

// AppendVarExport appends var_export($v, true) to dst.
func AppendVarExport(dst []byte, v any) []byte {
	return appendVarExport(dst, v, 1)
}

func appendVarExport(buf []byte, v any, level int) []byte {
	switch v := v.(type) {
	case nil:
		return append(buf, "NULL"...)
	case bool:
		if v {
			return append(buf, "true"...)
		}
		return append(buf, "false"...)
	case int64:
		return appendExportInt(buf, v)
	case int:
		return appendExportInt(buf, int64(v))
	case float64:
		return appendDouble(buf, v, -1, true)
	case string:
		return appendExportString(buf, v)
	case *Array:
		if level > 1 {
			buf = append(buf, '\n')
			buf = appendSpaces(buf, level-1)
		}
		buf = append(buf, "array (\n"...)
		for k, val := range v.All() {
			buf = appendSpaces(buf, level+1)
			if k.kind == kindInt {
				buf = strconv.AppendInt(buf, k.i, 10)
			} else {
				buf = appendExportString(buf, k.s)
			}
			buf = append(buf, " => "...)
			buf = appendVarExport(buf, val, level+2)
			buf = append(buf, ",\n"...)
		}
		if level > 1 {
			buf = appendSpaces(buf, level-1)
		}
		return append(buf, ')')
	case *Object:
		if level > 1 {
			buf = append(buf, '\n')
			buf = appendSpaces(buf, level-1)
		}
		buf = append(buf, "(object) array(\n"...)
		for name, val := range v.All() {
			buf = appendSpaces(buf, level+2)
			buf = appendExportString(buf, name)
			buf = append(buf, " => "...)
			buf = appendVarExport(buf, val, level+2)
			buf = append(buf, ",\n"...)
		}
		if level > 1 {
			buf = appendSpaces(buf, level-1)
		}
		return append(buf, ')')
	default:
		panic("php: unsupported value type " + TypeName(v))
	}
}

func appendExportInt(buf []byte, i int64) []byte {
	if i == math.MinInt64 {
		// The literal would parse as a float.
		return append(buf, "-9223372036854775807-1"...)
	}
	return strconv.AppendInt(buf, i, 10)
}

// appendExportString quotes s like var_export: ' and \ are backslashed and
// NUL bytes become ' . "\0" . '.
func appendExportString(buf []byte, s string) []byte {
	buf = append(buf, '\'')
	start := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '\\', 0:
			buf = append(buf, s[start:i]...)
			if c == 0 {
				buf = append(buf, `' . "\0" . '`...)
			} else {
				buf = append(buf, '\\', c)
			}
			start = i + 1
		}
	}
	buf = append(buf, s[start:]...)
	return append(buf, '\'')
}

func appendSpaces(buf []byte, n int) []byte {
	for range n {
		buf = append(buf, ' ')
	}
	return buf
}
