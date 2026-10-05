// Ports Process::prepareWindowsCommandLine from
// vendor/symfony/process/Process.php (5.4). It is platform independent so
// it is tested everywhere; only Windows runs it.

package util

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// prepareWindowsCommandLine moves each quoted argument holding ", %, ! or a
// newline into an environment variable referenced as !var! (cmd.exe runs
// with delayed expansion), then wraps the line for comSpec. It returns the
// command line and the NAME=value variables to add.
func prepareWindowsCommandLine(cmd, comSpec, uid string) (string, []string) {
	var (
		b        strings.Builder
		env      []string
		varCache = map[string]string{}
		varCount = 0
	)

	i := 0
	for i < len(cmd) {
		if cmd[i] != '"' {
			b.WriteByte(cmd[i])
			i++

			continue
		}

		start := i

		end, group, ok := matchWindowsQuoted(cmd, start)
		if !ok {
			b.WriteByte(cmd[i])
			i++

			continue
		}

		whole := cmd[start:end]
		i = end

		if group < 0 {
			b.WriteString(whole)

			continue
		}

		if cached, ok := varCache[whole]; ok {
			b.WriteString(cached)

			continue
		}

		value := strings.ReplaceAll(cmd[start+1:group], "\x00", "?")
		if !strings.ContainsAny(value, "\"%!\n") {
			b.WriteString(`"` + value + `"`)

			continue
		}

		for _, r := range [][2]string{{"!LF!", "\n"}, {`"^!"`, "!"}, {`"^%"`, "%"}, {`"^^"`, "^"}, {`""`, `"`}} {
			value = strings.ReplaceAll(value, r[0], r[1])
		}

		varCount++
		name := fmt.Sprintf("%s%d", uid, varCount)
		env = append(env, name+"="+`"`+escapeQuotesBackslashes(value)+`"`)
		ref := "!" + name + "!"
		varCache[whole] = ref
		b.WriteString(ref)
	}

	line := comSpec + " /V:ON /E:ON /D /C (" + strings.ReplaceAll(b.String(), "\n", " ") + ")"

	return line, env
}

// matchWindowsQuoted matches at s[start] == '"' the pattern
//
//	"(?:( [^"%!^]*+ (?: (?: !LF! | "(?:\^[%!^])?+" ) [^"%!^]*+ )++ ) | [^"]*+ )"
//
// returning the end of the match and the end of group 1 (-1 when the second
// alternative matched).
func matchWindowsQuoted(s string, start int) (end, groupEnd int, ok bool) {
	plainRun := func(i int) int {
		for i < len(s) && !strings.ContainsRune(`"%!^`, rune(s[i])) {
			i++
		}

		return i
	}

	// First alternative, every quantifier possessive.
	i := plainRun(start + 1)
	iterations := 0

	for {
		j, ok := matchWindowsToken(s, i)
		if !ok {
			break
		}

		i = plainRun(j)
		iterations++
	}

	if iterations > 0 && i < len(s) && s[i] == '"' {
		return i + 1, i, true
	}

	// Second alternative: [^"]*+ then the closing quote.
	if j := strings.IndexByte(s[start+1:], '"'); j >= 0 {
		return start + 1 + j + 1, -1, true
	}

	return 0, 0, false
}

// matchWindowsToken matches !LF! or "(?:\^[%!^])?+" at s[i].
func matchWindowsToken(s string, i int) (int, bool) {
	if strings.HasPrefix(s[i:], "!LF!") {
		return i + 4, true
	}

	if i >= len(s) || s[i] != '"' {
		return 0, false
	}

	j := i + 1
	if j+1 < len(s) && s[j] == '^' && strings.IndexByte("%!^", s[j+1]) >= 0 {
		j += 2
	}

	if j < len(s) && s[j] == '"' {
		return j + 1, true
	}

	return 0, false
}

// escapeQuotesBackslashes ports preg_replace('/(\\\\*)"/', '$1$1\\"', $s):
// each double quote is backslash-escaped and the backslashes before it
// doubled, per CommandLineToArgvW.
func escapeQuotesBackslashes(s string) string {
	if !strings.Contains(s, `"`) {
		return s
	}

	var b strings.Builder

	b.Grow(len(s) + 8)

	run := 0

	for i := range len(s) {
		switch c := s[i]; c {
		case '\\':
			run++
		case '"':
			b.WriteString(strings.Repeat(`\`, run*2) + `\"`)
			run = 0
		default:
			b.WriteString(strings.Repeat(`\`, run))
			b.WriteByte(c)
			run = 0
		}
	}

	b.WriteString(strings.Repeat(`\`, run))

	return b.String()
}

// quoteComSpec escapes the cmd.exe path according to CommandLineToArgvW.
func quoteComSpec(comSpec string, found bool) string {
	if !found {
		return "cmd"
	}

	return `"` + escapeQuotesBackslashes(comSpec) + `"`
}

// windowsComSpec finds cmd.exe once, as Symfony caches it statically.
var windowsComSpec = sync.OnceValues(func() (string, bool) {
	return NewExecutableFinder().Find("cmd.exe")
})

// newUniqid mimics uniqid('', true): the time in hex, a dot and digits.
func newUniqid() string {
	now := time.Now()

	return fmt.Sprintf("%08x%05x.%08d", now.Unix(), now.Nanosecond()/1000, now.UnixNano()%100000000)
}
