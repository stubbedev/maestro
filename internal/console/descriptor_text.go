// Ports src/Descriptor/TextDescriptor.php (symfony/console).

package console

import (
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// TextDescriptor renders the txt format.
type TextDescriptor struct {
	descriptorBase
}

// Describe implements Descriptor.
func (d *TextDescriptor) Describe(out Output, object any, options DescriptorOptions) error {
	return d.describe(d, out, object, options)
}

func (d *TextDescriptor) describeInputArgument(argument *InputArgument, options DescriptorOptions) {
	def := argument.Default()
	defaultText := ""
	if def != nil && (!isArrayValue(def) || inputTruthy(def)) {
		defaultText = "<comment> [default: " + formatDefaultValueJSON(def) + "]</comment>"
	}

	totalWidth := options.TotalWidth
	if totalWidth == 0 {
		totalWidth = Width(argument.Name())
	}
	spacingWidth := totalWidth - len(argument.Name())

	d.writeText("  <info>"+argument.Name()+"</info>  "+strings.Repeat(" ", max(0, spacingWidth))+
		// + 4 = 2 spaces before <info>, 2 spaces after </info>
		replaceNewlineRuns(argument.Description(), "\n"+strings.Repeat(" ", totalWidth+4))+
		defaultText, options)
}

func (d *TextDescriptor) describeInputOption(option *InputOption, options DescriptorOptions) {
	def := option.Default()
	defaultText := ""
	if option.AcceptValue() && def != nil && (!isArrayValue(def) || inputTruthy(def)) {
		defaultText = "<comment> [default: " + formatDefaultValueJSON(def) + "]</comment>"
	}

	value := ""
	if option.AcceptValue() {
		value = "=" + php.Strtoupper(option.Name())

		if option.IsValueOptional() {
			value = "[" + value + "]"
		}
	}

	totalWidth := options.TotalWidth
	if totalWidth == 0 {
		totalWidth = calculateTotalWidthForOptions([]*InputOption{option})
	}
	synopsis := "    "
	if option.shortcutIsTruthy() {
		synopsis = "-" + option.Shortcut() + ", "
	}
	if option.IsNegatable() {
		synopsis += "--" + option.Name() + "|--no-" + option.Name()
	} else {
		synopsis += "--" + option.Name() + value
	}

	spacingWidth := totalWidth - Width(synopsis)

	multiple := ""
	if option.IsArray() {
		multiple = "<comment> (multiple values allowed)</comment>"
	}

	d.writeText("  <info>"+synopsis+"</info>  "+strings.Repeat(" ", max(0, spacingWidth))+
		// + 4 = 2 spaces before <info>, 2 spaces after </info>
		replaceNewlineRuns(option.Description(), "\n"+strings.Repeat(" ", totalWidth+4))+
		defaultText+multiple, options)
}

func (d *TextDescriptor) describeInputDefinition(definition *InputDefinition, options DescriptorOptions) {
	totalWidth := calculateTotalWidthForOptions(definition.Options())
	for _, argument := range definition.Arguments() {
		totalWidth = max(totalWidth, Width(argument.Name()))
	}

	if len(definition.Arguments()) > 0 {
		d.writeText("<comment>Arguments:</comment>", options)
		d.writeText("\n", DescriptorOptions{})
		for _, argument := range definition.Arguments() {
			o := options
			o.TotalWidth = totalWidth
			d.describeInputArgument(argument, o)
			d.writeText("\n", DescriptorOptions{})
		}
	}

	if len(definition.Arguments()) > 0 && len(definition.Options()) > 0 {
		d.writeText("\n", DescriptorOptions{})
	}

	if len(definition.Options()) > 0 {
		var laterOptions []*InputOption

		d.writeText("<comment>Options:</comment>", options)
		for _, option := range definition.Options() {
			if len(option.Shortcut()) > 1 {
				laterOptions = append(laterOptions, option)

				continue
			}
			d.writeText("\n", DescriptorOptions{})
			o := options
			o.TotalWidth = totalWidth
			d.describeInputOption(option, o)
		}
		for _, option := range laterOptions {
			d.writeText("\n", DescriptorOptions{})
			o := options
			o.TotalWidth = totalWidth
			d.describeInputOption(option, o)
		}
	}
}

func (d *TextDescriptor) describeCommand(command Commander, options DescriptorOptions) {
	base := command.Base()
	_ = base.MergeApplicationDefinition(false)

	description := base.Description()
	if php.Truthy(description) {
		d.writeText("<comment>Description:</comment>", options)
		d.writeText("\n", DescriptorOptions{})
		d.writeText("  "+description, DescriptorOptions{})
		d.writeText("\n\n", DescriptorOptions{})
	}

	d.writeText("<comment>Usage:</comment>", options)
	usages := append([]string{base.Synopsis(true)}, base.Aliases()...)
	usages = append(usages, base.Usages()...)
	for _, usage := range usages {
		d.writeText("\n", DescriptorOptions{})
		d.writeText("  "+Escape(usage), options)
	}
	d.writeText("\n", DescriptorOptions{})

	definition := base.Definition()
	if len(definition.Options()) > 0 || len(definition.Arguments()) > 0 {
		d.writeText("\n", DescriptorOptions{})
		d.describeInputDefinition(definition, options)
		d.writeText("\n", DescriptorOptions{})
	}

	help := base.ProcessedHelp()
	if php.Truthy(help) && help != description {
		d.writeText("\n", DescriptorOptions{})
		d.writeText("<comment>Help:</comment>", options)
		d.writeText("\n", DescriptorOptions{})
		d.writeText("  "+strings.ReplaceAll(help, "\n", "\n  "), options)
		d.writeText("\n", DescriptorOptions{})
	}
}

func (d *TextDescriptor) describeApplication(app *Application, options DescriptorOptions) error {
	describedNamespace := options.Namespace
	description := NewApplicationDescription(app, describedNamespace, false)

	if options.RawText {
		names, commands, err := description.Commands()
		if err != nil {
			return err
		}
		cmds := make([]Commander, len(names))
		for i, n := range names {
			cmds[i] = commands[n]
		}
		width := columnWidthForCommands(cmds)
		for _, c := range cmds {
			base := c.Base()
			d.writeText(strPadRight(base.Name(), width)+" "+base.Description(), options)
			d.writeText("\n", DescriptorOptions{})
		}

		return nil
	}

	if help := app.help(); help != "" {
		d.writeText(help+"\n\n", options)
	}

	d.writeText("<comment>Usage:</comment>\n", options)
	d.writeText("  command [options] [arguments]\n\n", options)

	globalOptions := &InputDefinition{}
	_ = globalOptions.SetOptions(app.Definition().Options()...)
	d.describeInputDefinition(globalOptions, options)

	d.writeText("\n", DescriptorOptions{})
	d.writeText("\n", DescriptorOptions{})

	_, commands, err := description.Commands()
	if err != nil {
		return err
	}
	namespaces, err := description.Namespaces()
	if err != nil {
		return err
	}
	listed := commands
	if php.Truthy(describedNamespace) && len(namespaces) > 0 {
		// make sure all alias commands are included when describing a specific namespace
		listed = maps.Clone(commands)
		for _, name := range namespaces[0].Commands {
			c, err := description.Command(name)
			if err != nil {
				return err
			}
			listed[name] = c
		}
	}

	// calculate max. width based on available commands per namespace
	var widthNames []string
	for _, ns := range namespaces {
		for _, n := range ns.Commands {
			if _, ok := listed[n]; ok {
				widthNames = append(widthNames, n)
			}
		}
	}
	width := columnWidthForNames(widthNames)

	if php.Truthy(describedNamespace) {
		d.writeText(`<comment>Available commands for the "`+describedNamespace+`" namespace:</comment>`, options)
	} else {
		d.writeText("<comment>Available commands:</comment>", options)
	}

	for _, ns := range namespaces {
		nsCommands := slices.DeleteFunc(slices.Clone(ns.Commands), func(n string) bool {
			_, ok := listed[n]

			return !ok
		})

		if len(nsCommands) == 0 {
			continue
		}

		if (!php.Truthy(describedNamespace)) && ns.ID != GlobalNamespace {
			d.writeText("\n", DescriptorOptions{})
			d.writeText(" <comment>"+ns.ID+"</comment>", options)
		}

		for _, name := range nsCommands {
			d.writeText("\n", DescriptorOptions{})
			spacingWidth := width - Width(name)
			command := listed[name]
			commandAliases := ""
			if isCommandKey(command.Base().Name(), name) {
				commandAliases = commandAliasesText(command)
			}
			d.writeText("  <info>"+name+"</info>"+strings.Repeat(" ", max(0, spacingWidth))+commandAliases+command.Base().Description(), options)
		}
	}

	d.writeText("\n", DescriptorOptions{})

	return nil
}

func (d *TextDescriptor) writeText(content string, options DescriptorOptions) {
	if options.RawText {
		content = StripTags(content)
	}
	decorated := true
	if options.RawOutput != nil {
		decorated = !*options.RawOutput
	}
	d.write(content, decorated)
}

func commandAliasesText(command Commander) string {
	aliases := command.Base().Aliases()
	if len(aliases) == 0 {
		return ""
	}

	return "[" + strings.Join(aliases, "|") + "] "
}

func columnWidthForCommands(commands []Commander) int {
	widths := make([]int, 0, len(commands))
	for _, c := range commands {
		widths = append(widths, Width(c.Base().Name()))
		for _, alias := range c.Base().Aliases() {
			widths = append(widths, Width(alias))
		}
	}
	if len(widths) == 0 {
		return 0
	}

	return slices.Max(widths) + 2
}

func columnWidthForNames(names []string) int {
	if len(names) == 0 {
		return 0
	}
	w := 0
	for _, n := range names {
		w = max(w, Width(n))
	}

	return w + 2
}

func calculateTotalWidthForOptions(options []*InputOption) int {
	totalWidth := 0
	for _, option := range options {
		// "-" + shortcut + ", --" + name
		nameLength := 1 + max(Width(option.Shortcut()), 1) + 4 + Width(option.Name())
		if option.IsNegatable() {
			nameLength += 6 + Width(option.Name()) // |--no- + name
		} else if option.AcceptValue() {
			valueLength := 1 + Width(option.Name()) // = + value
			if option.IsValueOptional() {
				valueLength += 2 // [ + ]
			}
			nameLength += valueLength
		}
		totalWidth = max(totalWidth, nameLength)
	}

	return totalWidth
}

// replaceNewlineRuns is preg_replace('/\s*[\r\n]\s*/', $replacement, $s):
// every whitespace run containing a line break is replaced.
func replaceNewlineRuns(s, replacement string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(replacement))
	for i := 0; i < len(s); {
		if !isCSpace(s[i]) {
			b.WriteByte(s[i])
			i++

			continue
		}
		j := i
		hasBreak := false
		for j < len(s) && isCSpace(s[j]) {
			if s[j] == '\r' || s[j] == '\n' {
				hasBreak = true
			}
			j++
		}
		if hasBreak {
			b.WriteString(replacement)
		} else {
			b.WriteString(s[i:j])
		}
		i = j
	}

	return b.String()
}
