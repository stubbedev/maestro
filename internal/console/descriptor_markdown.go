// Ports src/Descriptor/MarkdownDescriptor.php (symfony/console).

package console

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// MarkdownDescriptor renders the md format.
type MarkdownDescriptor struct {
	descriptorBase
}

// Describe implements Descriptor; the output is undecorated while
// describing.
func (d *MarkdownDescriptor) Describe(out Output, object any, options DescriptorOptions) error {
	decorated := out.IsDecorated()
	out.SetDecorated(false)
	defer out.SetDecorated(decorated)

	return d.describe(d, out, object, options)
}

// mdWrite is write($content, $decorated = true).
func (d *MarkdownDescriptor) mdWrite(content string) { d.write(content, true) }

// mdDefault is str_replace("\n", ”, var_export($default, true)).
func mdDefault(def any) string {
	return strings.ReplaceAll(php.VarExport(phpValue(def)), "\n", "")
}

func mdYesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}

func (d *MarkdownDescriptor) describeInputArgument(argument *InputArgument, _ DescriptorOptions) {
	name := argument.Name()
	if name == "" || name == "0" {
		name = "<none>"
	}
	description := ""
	if desc := argument.Description(); desc != "" && desc != "0" {
		description = replaceNewlineRuns(desc, "\n") + "\n\n"
	}

	d.mdWrite("#### `" + name + "`\n\n" +
		description +
		"* Is required: " + mdYesNo(argument.IsRequired()) + "\n" +
		"* Is array: " + mdYesNo(argument.IsArray()) + "\n" +
		"* Default: `" + mdDefault(argument.Default()) + "`")
}

func (d *MarkdownDescriptor) describeInputOption(option *InputOption, _ DescriptorOptions) {
	name := "--" + option.Name()
	if option.IsNegatable() {
		name += "|--no-" + option.Name()
	}
	if s := option.Shortcut(); s != "" && s != "0" {
		name += "|-" + strings.ReplaceAll(s, "|", "|-")
	}
	description := ""
	if desc := option.Description(); desc != "" && desc != "0" {
		description = replaceNewlineRuns(desc, "\n") + "\n\n"
	}

	d.mdWrite("#### `" + name + "`" + "\n\n" +
		description +
		"* Accept value: " + mdYesNo(option.AcceptValue()) + "\n" +
		"* Is value required: " + mdYesNo(option.IsValueRequired()) + "\n" +
		"* Is multiple: " + mdYesNo(option.IsArray()) + "\n" +
		"* Is negatable: " + mdYesNo(option.IsNegatable()) + "\n" +
		"* Default: `" + mdDefault(option.Default()) + "`")
}

func (d *MarkdownDescriptor) describeInputDefinition(definition *InputDefinition, options DescriptorOptions) {
	showArguments := len(definition.Arguments()) > 0
	if showArguments {
		d.mdWrite("### Arguments")
		for _, argument := range definition.Arguments() {
			d.mdWrite("\n\n")
			d.describeInputArgument(argument, options)
		}
	}

	if len(definition.Options()) > 0 {
		if showArguments {
			d.mdWrite("\n\n")
		}

		d.mdWrite("### Options")
		for _, option := range definition.Options() {
			d.mdWrite("\n\n")
			d.describeInputOption(option, options)
		}
	}
}

func mdUsages(usages []string) string {
	var b strings.Builder
	for _, usage := range usages {
		b.WriteString("* `" + usage + "`" + "\n")
	}

	return b.String()
}

func (d *MarkdownDescriptor) mdCommandHeader(base *Command, usages []string) string {
	description := ""
	if desc := base.Description(); desc != "" && desc != "0" {
		description = desc + "\n\n"
	}

	return "`" + base.Name() + "`\n" +
		strings.Repeat("-", Width(base.Name())+2) + "\n\n" +
		description +
		"### Usage" + "\n\n" +
		mdUsages(usages)
}

func (d *MarkdownDescriptor) describeCommand(command Commander, options DescriptorOptions) {
	base := command.Base()
	if options.Short {
		d.mdWrite(d.mdCommandHeader(base, base.Aliases()))

		return
	}

	_ = base.MergeApplicationDefinition(false)

	usages := make([]string, 0, 1+len(base.Aliases())+len(base.Usages()))
	usages = append(usages, base.Synopsis(false))
	usages = append(usages, base.Aliases()...)
	usages = append(usages, base.Usages()...)
	d.mdWrite(d.mdCommandHeader(base, usages))

	if help := base.ProcessedHelp(); help != "" && help != "0" {
		d.mdWrite("\n")
		d.mdWrite(help)
	}

	definition := base.Definition()
	if len(definition.Options()) > 0 || len(definition.Arguments()) > 0 {
		d.mdWrite("\n\n")
		d.describeInputDefinition(definition, DescriptorOptions{})
	}
}

func (d *MarkdownDescriptor) describeApplication(app *Application, options DescriptorOptions) error {
	describedNamespace := options.Namespace
	description := NewApplicationDescription(app, describedNamespace, false)
	title := markdownApplicationTitle(app)

	d.mdWrite(title + "\n" + strings.Repeat("=", Width(title)))

	namespaces, err := description.Namespaces()
	if err != nil {
		return err
	}
	for _, ns := range namespaces {
		if ns.ID != GlobalNamespace {
			d.mdWrite("\n\n")
			d.mdWrite("**" + ns.ID + ":**")
		}

		d.mdWrite("\n\n")
		lines := make([]string, len(ns.Commands))
		for i, commandName := range ns.Commands {
			command, err := description.Command(commandName)
			if err != nil {
				return err
			}
			lines[i] = "* [`" + commandName + "`](#" + strings.ReplaceAll(command.Base().Name(), ":", "") + ")"
		}
		d.mdWrite(strings.Join(lines, "\n"))
	}

	names, commands, err := description.Commands()
	if err != nil {
		return err
	}
	for _, name := range names {
		d.mdWrite("\n\n")
		d.describeCommand(commands[name], options)
	}

	return nil
}

func markdownApplicationTitle(app *Application) string {
	if app.Name() != "UNKNOWN" {
		if app.Version() != "UNKNOWN" {
			return app.Name() + " " + app.Version()
		}

		return app.Name()
	}

	return "Console Tool"
}
