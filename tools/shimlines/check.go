package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// annotationRe is Maestro\Shim\Traces::composerLine()'s pattern: the
// optional path relative to Composer's root, then the line.
var annotationRe = regexp.MustCompile(`// @line (?:(\S+):)?(\d+)`)

// annotation is one `@line` comment of the shim: where it is, the
// location in Composer's sources it names and the call it stands for.
type annotation struct {
	shimFile     string
	shimLine     int
	spec         string
	composerFile string
	composerLine int
	call         call
}

// drift is an annotation whose Composer line does not make its call,
// with the lines of that file that do (none when the call is gone).
type drift struct {
	annotation

	found []int
}

func (d drift) String() string {
	at := fmt.Sprintf("%s:%d: @line %s: %s:%d does not call %s", d.shimFile, d.shimLine, d.spec, d.composerFile, d.composerLine, d.call)
	switch len(d.found) {
	case 0:
		return fmt.Sprintf("%s, and no line of %s does", at, d.composerFile)
	case 1:
		return fmt.Sprintf("%s; it is at line %d", at, d.found[0])
	default:
		lines := make([]string, len(d.found))
		for i, n := range d.found {
			lines[i] = strconv.Itoa(n)
		}

		return fmt.Sprintf("%s; it is at lines %s", at, strings.Join(lines, ", "))
	}
}

// check reads every `@line` annotation of the PHP files under src, in
// lexical order, and returns those whose line of Composer's checkout ref
// does not make the annotated call.
func check(ref, src string) ([]drift, error) {
	annotations, err := readAnnotations(src)
	if err != nil {
		return nil, err
	}

	sources := map[string][]string{}
	var drifts []drift
	for _, a := range annotations {
		lines, ok := sources[a.composerFile]
		if !ok {
			lines, err = readLines(filepath.Join(ref, filepath.FromSlash(a.composerFile)))
			if err != nil {
				return nil, fmt.Errorf("%s:%d: @line %s: %w", a.shimFile, a.shimLine, a.spec, err)
			}
			sources[a.composerFile] = lines
		}
		if a.composerLine <= len(lines) && makes(lines[a.composerLine-1], a.call) {
			continue
		}
		d := drift{annotation: a}
		for i, line := range lines {
			if makes(line, a.call) {
				d.found = append(d.found, i+1)
			}
		}
		drifts = append(drifts, d)
	}

	return drifts, nil
}

// readAnnotations returns the `@line` annotations of the PHP files under
// src, in lexical order of file and line.
func readAnnotations(src string) ([]annotation, error) {
	var annotations []annotation
	err := filepath.WalkDir(src, func(file string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || filepath.Ext(file) != ".php" {
			return err
		}
		rel, err := filepath.Rel(src, file)
		if err != nil {
			return err
		}
		lines, err := readLines(file)
		if err != nil {
			return err
		}
		for i, line := range lines {
			m := annotationRe.FindStringSubmatchIndex(line)
			if m == nil {
				continue
			}
			a, err := parseAnnotation(file, filepath.ToSlash(rel), i+1, line, m)
			if err != nil {
				return err
			}
			annotations = append(annotations, a)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading the @line annotations under %s: %w", src, err)
	}

	return annotations, nil
}

// parseAnnotation reads the annotation annotationRe matched (m) on line n
// of the shim file at rel under src/: the Composer file it names (rel
// under Composer's src/ when it names none), its line, and the call the
// code before it makes.
func parseAnnotation(file, rel string, n int, line string, m []int) (annotation, error) {
	spec := line[m[0]+len("// @line ") : m[1]]
	composerLine, err := strconv.Atoi(line[m[4]:m[5]])
	if err != nil || composerLine < 1 {
		return annotation{}, fmt.Errorf("%s:%d: @line %s names no line", file, n, spec)
	}
	composerFile := path.Join("src", rel)
	if m[2] >= 0 {
		composerFile = line[m[2]:m[3]]
	}
	c, ok := annotatedCall(line[:m[0]])
	if !ok {
		return annotation{}, fmt.Errorf("%s:%d: @line %s annotates a line that makes no call", file, n, spec)
	}

	return annotation{
		shimFile:     file,
		shimLine:     n,
		spec:         spec,
		composerFile: composerFile,
		composerLine: composerLine,
		call:         c,
	}, nil
}

// readLines returns the lines of file.
func readLines(file string) ([]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	return strings.Split(string(data), "\n"), nil
}
