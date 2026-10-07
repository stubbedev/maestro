package archive

import (
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// localeFromEnv classifies LC_CTYPE as setlocale(LC_CTYPE, "") would pick
// it: LC_ALL, then LC_CTYPE, then LANG, the first one that is set. A UTF-8
// locale is assumed to be installed (glibc ships C.UTF-8 built in).
func localeFromEnv() Locale {
	name := ""
	for _, v := range [...]string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if name = os.Getenv(v); name != "" {
			break
		}
	}

	return classifyLocale(name)
}

func classifyLocale(name string) Locale {
	if name == "" || name == "C" || name == "POSIX" {
		return LocaleC
	}

	_, codeset, ok := strings.Cut(name, ".")
	if !ok {
		return LocaleOther
	}

	codeset, _, _ = strings.Cut(codeset, "@")
	codeset = strings.NewReplacer("-", "", "_", "").Replace(php.Strtolower(codeset))

	if codeset == "utf8" {
		return LocaleUTF8
	}

	return LocaleOther
}
