// Ports src/Descriptor/JsonDescriptor.php (symfony/console).

package console

import (
	"math"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// JSONDescriptor renders the json format.
type JSONDescriptor struct {
	descriptorBase
}

// Describe implements Descriptor.
func (d *JSONDescriptor) Describe(out Output, object any, options DescriptorOptions) error {
	return d.describe(d, out, object, options)
}

func (d *JSONDescriptor) describeInputArgument(argument *InputArgument, options DescriptorOptions) {
	d.writeData(inputArgumentData(argument), options)
}

func (d *JSONDescriptor) describeInputOption(option *InputOption, options DescriptorOptions) {
	d.writeData(inputOptionData(option, false), options)
	if option.IsNegatable() {
		d.writeData(inputOptionData(option, true), options)
	}
}

func (d *JSONDescriptor) describeInputDefinition(definition *InputDefinition, options DescriptorOptions) {
	d.writeData(inputDefinitionData(definition), options)
}

func (d *JSONDescriptor) describeCommand(command Commander, options DescriptorOptions) {
	d.writeData(commandData(command, options.Short), options)
}

func (d *JSONDescriptor) describeApplication(app *Application, options DescriptorOptions) error {
	describedNamespace := options.Namespace
	description := NewApplicationDescription(app, describedNamespace, true)
	names, cmds, err := description.Commands()
	if err != nil {
		return err
	}

	commands := php.NewArrayCap(len(names))
	for _, name := range names {
		commands.Append(commandData(cmds[name], options.Short))
	}

	data := php.NewArray()
	if app.Name() != "UNKNOWN" {
		application := php.ArrayOf("name", app.Name())
		if app.Version() != "UNKNOWN" {
			application.Set("version", app.Version())
		}
		data.Set("application", application)
	}

	data.Set("commands", commands)

	if describedNamespace != "" && describedNamespace != "0" {
		data.Set("namespace", describedNamespace)
	} else {
		namespaces, err := description.Namespaces()
		if err != nil {
			return err
		}
		list := php.NewArrayCap(len(namespaces))
		for _, ns := range namespaces {
			// ids and names come from array keys: canonical integers are ints.
			names := php.NewArrayCap(len(ns.Commands))
			for _, name := range ns.Commands {
				names.Append(php.StrKey(name).Value())
			}
			list.Append(php.ArrayOf("id", php.StrKey(ns.ID).Value(), "commands", names))
		}
		data.Set("namespaces", list)
	}

	d.writeData(data, options)

	return nil
}

// writeData writes json_encode($data, $options['json_encoding'] ?? 0); a
// failed encoding writes "" like PHP writing false.
func (d *JSONDescriptor) writeData(data *php.Array, options DescriptorOptions) {
	s, err := php.JSONEncode(data, options.JSONEncoding)
	if err != nil {
		s = ""
	}
	d.write(s, false)
}

// jsonDefault is "\INF === $default ? 'INF' : $default".
func jsonDefault(def any) any {
	if f, ok := def.(float64); ok && math.IsInf(f, 1) {
		return "INF"
	}

	return phpValue(def)
}

func inputArgumentData(argument *InputArgument) *php.Array {
	return php.ArrayOf(
		"name", argument.Name(),
		"is_required", argument.IsRequired(),
		"is_array", argument.IsArray(),
		"description", replaceNewlineRuns(argument.Description(), " "),
		"default", jsonDefault(argument.Default()),
	)
}

func inputOptionData(option *InputOption, negated bool) *php.Array {
	if negated {
		return php.ArrayOf(
			"name", "--no-"+option.Name(),
			"shortcut", "",
			"accept_value", false,
			"is_value_required", false,
			"is_multiple", false,
			"description", `Negate the "--`+option.Name()+`" option`,
			"default", false,
		)
	}

	shortcut := ""
	if s := option.Shortcut(); s != "" && s != "0" {
		shortcut = "-" + strings.ReplaceAll(s, "|", "|-")
	}

	return php.ArrayOf(
		"name", "--"+option.Name(),
		"shortcut", shortcut,
		"accept_value", option.AcceptValue(),
		"is_value_required", option.IsValueRequired(),
		"is_multiple", option.IsArray(),
		"description", replaceNewlineRuns(option.Description(), " "),
		"default", jsonDefault(option.Default()),
	)
}

func inputDefinitionData(definition *InputDefinition) *php.Array {
	inputArguments := php.NewArrayCap(len(definition.Arguments()))
	for _, argument := range definition.Arguments() {
		inputArguments.Set(argument.Name(), inputArgumentData(argument))
	}

	inputOptions := php.NewArrayCap(len(definition.Options()))
	for _, option := range definition.Options() {
		inputOptions.Set(option.Name(), inputOptionData(option, false))
		if option.IsNegatable() {
			inputOptions.Set("no-"+option.Name(), inputOptionData(option, true))
		}
	}

	return php.ArrayOf("arguments", inputArguments, "options", inputOptions)
}

func commandData(command Commander, short bool) *php.Array {
	base := command.Base()
	data := php.ArrayOf(
		"name", base.Name(),
		"description", base.Description(),
	)

	if short {
		data.Set("usage", php.StringList(base.Aliases()))
	} else {
		_ = base.MergeApplicationDefinition(false)

		usage := make([]string, 0, 1+len(base.Usages())+len(base.Aliases()))
		usage = append(usage, base.Synopsis(false))
		usage = append(usage, base.Usages()...)
		usage = append(usage, base.Aliases()...)
		data.Set("usage", php.StringList(usage))
		data.Set("help", base.ProcessedHelp())
		data.Set("definition", inputDefinitionData(base.Definition()))
	}

	data.Set("hidden", base.IsHidden())

	return data
}
