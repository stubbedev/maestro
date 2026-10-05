// Ports src/Composer/Console/HtmlOutputFormatter.php (Composer).

package console

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// HTMLOutputFormatter renders decorated output as HTML spans.
type HTMLOutputFormatter struct {
	*OutputFormatter
}

// NewHTMLOutputFormatter mirrors new HtmlOutputFormatter($styles): an
// always-decorated OutputFormatter.
func NewHTMLOutputFormatter(styles ...NamedStyle) *HTMLOutputFormatter {
	return &HTMLOutputFormatter{OutputFormatter: NewOutputFormatter(true, styles...)}
}

var htmlForegroundColors = map[int]string{
	30: "black", 31: "red", 32: "green", 33: "yellow",
	34: "blue", 35: "magenta", 36: "cyan", 37: "white",
}

var htmlBackgroundColors = map[int]string{
	40: "black", 41: "red", 42: "green", 43: "yellow",
	44: "blue", 45: "magenta", 46: "cyan", 47: "white",
}

const htmlClearEscapeCodes = `(?:39|49|0|22|24|25|27|28)`

var htmlEscapeRe = php.MustCompile("{\033\\[([0-9;]+)m(.*?)\033\\[(?:" + htmlClearEscapeCodes + ";)*?" + htmlClearEscapeCodes + "m}s")

// Format implements Formatter.
func (f *HTMLOutputFormatter) Format(message string) string {
	formatted := f.OutputFormatter.Format(message)

	out, _, err := htmlEscapeRe.ReplaceCallback(formatted, func(m *php.Match) string {
		return formatHTML(m.Get(1), m.Get(2))
	}, -1)
	if err != nil {
		// Preg::replaceCallback throws; Format has no error result, so the
		// PcreException escapes like the formatter's other throws.
		panic(err)
	}

	return out
}

func formatHTML(codes, text string) string {
	var out strings.Builder
	out.WriteString(`<span style="`)
	for c := range strings.SplitSeq(codes, ";") {
		code := phpIntval(c)
		if name, ok := htmlForegroundColors[code]; ok {
			out.WriteString("color:" + name + ";")
		} else if name, ok := htmlBackgroundColors[code]; ok {
			out.WriteString("background-color:" + name + ";")
		} else {
			switch code {
			case 1:
				out.WriteString("font-weight:bold;")
			case 4:
				out.WriteString("text-decoration:underline;")
			}
		}
	}
	out.WriteString(`">`)
	out.WriteString(text)
	out.WriteString("</span>")

	return out.String()
}
