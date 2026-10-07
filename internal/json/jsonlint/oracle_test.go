package jsonlint

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/testutil"
)

// oracleString is a string the oracle stores as is, or as {"b64": ...}
// when it is not valid UTF-8.
type oracleString string

func (s *oracleString) UnmarshalJSON(data []byte) error {
	var b struct {
		B64 string `json:"b64"`
	}
	if len(data) > 0 && data[0] == '{' {
		if err := json.Unmarshal(data, &b); err != nil {
			return err
		}
		raw, err := base64.StdEncoding.DecodeString(b.B64)
		*s = oracleString(raw)

		return err
	}
	var str string
	err := json.Unmarshal(data, &str)
	*s = oracleString(str)

	return err
}

// oracleResult is the outcome of one parse or validation in PHP.
type oracleResult struct {
	Flags   int             `json:"flags"`
	Value   *oracleString   `json:"value"`
	MD5     string          `json:"md5"`
	Class   string          `json:"class"`
	Message oracleString    `json:"message"`
	Details json.RawMessage `json:"details"`
}

type oracleCase struct {
	Input   oracleString   `json:"input"`
	Results []oracleResult `json:"results"`
}

type oracleUTF8 struct {
	Input oracleString `json:"input"`
	oracleResult
}

func loadOracle(t *testing.T) (parse []oracleCase, utf8 []oracleUTF8) {
	t.Helper()
	data, err := testutil.ReadGoldenFile("testdata/oracle/jsonlint.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Parse []oracleCase `json:"parse"`
		UTF8  []oracleUTF8 `json:"utf8"`
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}

	return golden.Parse, golden.UTF8
}

// errorClass returns the short PHP class name of an error Parse returns.
func errorClass(err error) string {
	if phperr.Of(err) == nil {
		return "?"
	}
	class := phperr.Class(err)

	return class[strings.LastIndexByte(class, '\\')+1:]
}

// checkResult compares the Go outcome (v, err) with the PHP one.
func checkResult(t *testing.T, name string, want *oracleResult, v any, err error) {
	t.Helper()
	if want.Class != "" {
		if err == nil {
			t.Errorf("%s: got value %s, want %s: %q", name, php.VarExport(v), want.Class, want.Message)

			return
		}
		if got := errorClass(err); got != want.Class || err.Error() != string(want.Message) {
			t.Errorf("%s: got %s: %q\nwant %s: %q", name, got, err.Error(), want.Class, want.Message)

			return
		}
		if want.Details == nil {
			return
		}
		var pe *ParsingError
		if !errors.As(err, &pe) {
			t.Errorf("%s: %T carries no details", name, err)

			return
		}
		got, jerr := php.JSONEncode(pe.Details.Array(), php.JSONUnescapedSlashes|php.JSONUnescapedUnicode|php.JSONInvalidUTF8Substitute)
		if jerr != nil || got != string(want.Details) {
			t.Errorf("%s: details\ngot  %s (%v)\nwant %s", name, got, jerr, want.Details)
		}

		return
	}
	if err != nil {
		t.Errorf("%s: got error %T %q, want a value", name, err, err.Error())

		return
	}
	export := php.VarExport(v)
	if want.MD5 != "" {
		sum := md5.Sum([]byte(export))
		if hex.EncodeToString(sum[:]) != want.MD5 {
			t.Errorf("%s: value md5 mismatch", name)
		}

		return
	}
	if want.Value == nil || export != string(*want.Value) {
		t.Errorf("%s: got value %s, want %v", name, export, want.Value)
	}
}

func TestOracleParse(t *testing.T) {
	cases, _ := loadOracle(t)
	n := 0
	for i, c := range cases {
		for j := range c.Results {
			want := &c.Results[j]
			v, err := Parse(string(c.Input), want.Flags)
			checkResult(t, "case "+itoa(i)+" flags "+itoa(want.Flags)+" "+php.VarExport(string(c.Input)), want, v, err)
			n++
		}
	}
	t.Logf("%d inputs, %d parses", len(cases), n)
}

func TestOracleUtf8Validator(t *testing.T) {
	_, cases := loadOracle(t)
	for i, c := range cases {
		err := ValidateUTF8(string(c.Input))
		checkResult(t, "utf8 case "+itoa(i), &c.oracleResult, nil, err)
	}
}
