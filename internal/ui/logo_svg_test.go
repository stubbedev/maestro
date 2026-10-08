package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/switches"
)

// logoSVGFile is the README's logo, drawn from wordmark and the gradient's
// ends so it cannot drift from what the console shows.
const logoSVGFile = "../../docs/assets/maestro-logo.svg"

// The logo's cell is a monospace character's box; a box-drawing
// character's double line is two strokes logoGap off its centre line.
const (
	logoCellW  = 10.0
	logoCellH  = 20.0
	logoGap    = 2.2
	logoStroke = 1.4
)

// logoSVG draws wordmark as SVG: a block as a filled cell, a box-drawing
// character as its double line, all filled with the console's gradient
// running left to right across the whole width.
func logoSVG() (string, error) {
	lines := strings.Split(wordmark, "\n")
	cols := 0
	for _, line := range lines {
		cols = max(cols, len([]rune(line)))
	}

	num := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	var blocks, strokes []string
	seg := func(pts ...[2]float64) {
		parts := make([]string, len(pts))
		for i, p := range pts {
			parts[i] = num(p[0]) + " " + num(p[1])
		}
		strokes = append(strokes, "M"+strings.Join(parts, " L"))
	}

	const w, h, d = logoCellW, logoCellH, logoGap
	const m, c = w / 2, h / 2
	for row, line := range lines {
		for col, r := range []rune(line) {
			x, y := float64(col)*w, float64(row)*h
			switch r {
			case ' ':
			case '█':
				blocks = append(blocks, fmt.Sprintf("M%s %sh%sv%sh-%sz", num(x), num(y), num(w), num(h), num(w)))
			case '═':
				seg([2]float64{x, y + c - d}, [2]float64{x + w, y + c - d})
				seg([2]float64{x, y + c + d}, [2]float64{x + w, y + c + d})
			case '║':
				seg([2]float64{x + m - d, y}, [2]float64{x + m - d, y + h})
				seg([2]float64{x + m + d, y}, [2]float64{x + m + d, y + h})
			case '╗':
				seg([2]float64{x, y + c - d}, [2]float64{x + m + d, y + c - d}, [2]float64{x + m + d, y + h})
				seg([2]float64{x, y + c + d}, [2]float64{x + m - d, y + c + d}, [2]float64{x + m - d, y + h})
			case '╔':
				seg([2]float64{x + w, y + c - d}, [2]float64{x + m - d, y + c - d}, [2]float64{x + m - d, y + h})
				seg([2]float64{x + w, y + c + d}, [2]float64{x + m + d, y + c + d}, [2]float64{x + m + d, y + h})
			case '╚':
				seg([2]float64{x + m - d, y}, [2]float64{x + m - d, y + c + d}, [2]float64{x + w, y + c + d})
				seg([2]float64{x + m + d, y}, [2]float64{x + m + d, y + c - d}, [2]float64{x + w, y + c - d})
			case '╝':
				seg([2]float64{x + m + d, y}, [2]float64{x + m + d, y + c + d}, [2]float64{x, y + c + d})
				seg([2]float64{x + m - d, y}, [2]float64{x + m - d, y + c - d}, [2]float64{x, y + c - d})
			default:
				return "", fmt.Errorf("wordmark row %d column %d: no drawing for %q", row, col, r)
			}
		}
	}

	width, height := num(float64(cols)*w), num(float64(len(lines))*h)
	hex := func(c rgb) string { return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b) }

	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ` + width + " " + height + `" role="img" aria-label="maestro">
  <title>maestro</title>
  <defs>
    <linearGradient id="g" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="` + width + `" y2="0">
      <stop offset="0" stop-color="` + hex(logoFrom) + `"/>
      <stop offset="1" stop-color="` + hex(logoTo) + `"/>
    </linearGradient>
  </defs>
  <path fill="url(#g)" d="` + strings.Join(blocks, "") + `"/>
  <path fill="none" stroke="url(#g)" stroke-width="` + num(logoStroke) + `" stroke-linecap="square" d="` + strings.Join(strokes, " ") + `"/>
</svg>
`, nil
}

// TestLogoSVG: docs/assets/maestro-logo.svg is the wordmark the console
// shows, in its colours. After a change to either, run this test with
// MAESTRO_UPDATE_LOGO=1 (`just logo`) to redraw it.
func TestLogoSVG(t *testing.T) {
	want, err := logoSVG()
	if err != nil {
		t.Fatal(err)
	}
	if switches.On(switches.UpdateLogo) {
		if err := os.WriteFile(logoSVGFile, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}

		return
	}
	got, err := os.ReadFile(logoSVGFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s is not the console's wordmark: run `just logo` (MAESTRO_UPDATE_LOGO=1) to redraw it", logoSVGFile)
	}
}
