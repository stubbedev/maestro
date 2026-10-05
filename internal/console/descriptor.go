// Ports src/Descriptor/DescriptorInterface.php, Descriptor.php,
// ApplicationDescription.php and src/Helper/DescriptorHelper.php
// (symfony/console).

package console

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// DescriptorOptions are the describe() options.
type DescriptorOptions struct {
	Format    string // "" means "txt" (an unset option)
	RawText   bool
	Namespace string
	Short     bool
	// RawOutput is the 'raw_output' option; nil means unset.
	RawOutput *bool
	// TotalWidth is the internal 'total_width' option; 0 means unset.
	TotalWidth int
	// JSONEncoding is the 'json_encoding' option: the json_encode flags of
	// the json format.
	JSONEncoding php.JSONFlag
}

// Descriptor is DescriptorInterface. object is an *InputArgument,
// *InputOption, *InputDefinition, Commander or *Application.
type Descriptor interface {
	Describe(out Output, object any, options DescriptorOptions) error
}

// describer is implemented by the concrete descriptors; descriptorBase
// dispatches to it like the abstract Descriptor class.
type describer interface {
	describeInputArgument(argument *InputArgument, options DescriptorOptions)
	describeInputOption(option *InputOption, options DescriptorOptions)
	describeInputDefinition(definition *InputDefinition, options DescriptorOptions)
	describeCommand(command Commander, options DescriptorOptions)
	describeApplication(app *Application, options DescriptorOptions) error
}

type descriptorBase struct {
	output Output
}

func (d *descriptorBase) describe(self describer, out Output, object any, options DescriptorOptions) error {
	d.output = out

	switch o := object.(type) {
	case *InputArgument:
		self.describeInputArgument(o, options)
	case *InputOption:
		self.describeInputOption(o, options)
	case *InputDefinition:
		self.describeInputDefinition(o, options)
	case Commander:
		self.describeCommand(o, options)
	case *Application:
		return self.describeApplication(o, options)
	default:
		return newError(KindInvalidArgument, "Descriptor.php", 58, `Object of type "%s" is not describable.`, strings.TrimPrefix(fmt.Sprintf("%T", object), "*"))
	}

	return nil
}

func (d *descriptorBase) write(content string, decorated bool) {
	if decorated {
		d.output.Write(content, false, OutputNormal)
	} else {
		d.output.Write(content, false, OutputRaw)
	}
}

// DescriptorHelper describes objects in the registered formats.
type DescriptorHelper struct {
	HelperBase
	formats     []string
	descriptors map[string]Descriptor
}

// NewDescriptorHelper registers the txt, xml, json and md descriptors.
func NewDescriptorHelper() *DescriptorHelper {
	h := &DescriptorHelper{descriptors: map[string]Descriptor{}}
	h.Register("txt", &TextDescriptor{})
	h.Register("xml", &XMLDescriptor{})
	h.Register("json", &JSONDescriptor{})
	h.Register("md", &MarkdownDescriptor{})

	return h
}

// Describe describes object in options.Format.
func (h *DescriptorHelper) Describe(out Output, object any, options DescriptorOptions) error {
	if options.Format == "" {
		options.Format = "txt"
	}

	return h.describe(out, object, options)
}

// describe is Describe with the format taken as given: the help and list
// commands always pass their --format option, so "--format=" is an
// unsupported format as in PHP.
func (h *DescriptorHelper) describe(out Output, object any, options DescriptorOptions) error {
	d, ok := h.descriptors[options.Format]
	if !ok {
		return newError(KindInvalidArgument, "DescriptorHelper.php", 66, `Unsupported format "%s".`, options.Format)
	}

	return d.Describe(out, object, options)
}

// Register adds a descriptor for a format.
func (h *DescriptorHelper) Register(format string, d Descriptor) *DescriptorHelper {
	if _, ok := h.descriptors[format]; !ok {
		h.formats = append(h.formats, format)
	}
	h.descriptors[format] = d

	return h
}

// Name implements Helper.
func (*DescriptorHelper) Name() string { return "descriptor" }

// Formats returns the registered formats.
func (h *DescriptorHelper) Formats() []string { return h.formats }

// GlobalNamespace is ApplicationDescription::GLOBAL_NAMESPACE.
const GlobalNamespace = "_global"

// DescribedNamespace is one namespace entry of an ApplicationDescription.
type DescribedNamespace struct {
	ID       string
	Commands []string
}

// ApplicationDescription groups an application's commands by namespace.
type ApplicationDescription struct {
	application *Application
	namespace   string
	showHidden  bool

	inspected    bool
	namespaces   []DescribedNamespace
	commandNames []string
	commands     map[string]Commander
	aliases      map[string]Commander
}

// NewApplicationDescription describes app, optionally one namespace.
func NewApplicationDescription(app *Application, namespace string, showHidden bool) *ApplicationDescription {
	return &ApplicationDescription{application: app, namespace: namespace, showHidden: showHidden}
}

// Namespaces returns the namespaces in display order.
func (d *ApplicationDescription) Namespaces() ([]DescribedNamespace, error) {
	if err := d.inspect(); err != nil {
		return nil, err
	}

	return d.namespaces, nil
}

// Commands returns the command names (not aliases) in display order, and
// the commands by name.
func (d *ApplicationDescription) Commands() ([]string, map[string]Commander, error) {
	if err := d.inspect(); err != nil {
		return nil, nil, err
	}

	return d.commandNames, d.commands, nil
}

// Command returns a command by name or alias.
func (d *ApplicationDescription) Command(name string) (Commander, error) {
	if c, ok := d.commands[name]; ok {
		return c, nil
	}
	if c, ok := d.aliases[name]; ok {
		return c, nil
	}

	return nil, newError(KindCommandNotFound, "ApplicationDescription.php", 80, `Command "%s" does not exist.`, name)
}

func (d *ApplicationDescription) inspect() error {
	if d.inspected {
		return nil
	}

	ns := ""
	if d.namespace != "" && d.namespace != "0" {
		var err error
		if ns, err = d.application.FindNamespace(d.namespace); err != nil {
			return err
		}
	}
	d.inspected = true
	d.commands = map[string]Commander{}
	d.aliases = map[string]Commander{}

	all := d.application.All(ns)
	for _, group := range d.sortCommands(all) {
		var names []string
		for _, nc := range group.commands {
			base := nc.Command.Base()
			if base.Name() == "" || base.Name() == "0" || (!d.showHidden && base.IsHidden()) {
				continue
			}

			if isCommandKey(base.Name(), nc.Name) {
				d.commands[nc.Name] = nc.Command
				d.commandNames = append(d.commandNames, nc.Name)
			} else {
				d.aliases[nc.Name] = nc.Command
			}

			names = append(names, nc.Name)
		}

		d.namespaces = append(d.namespaces, DescribedNamespace{ID: group.id, Commands: names})
	}

	return nil
}

type commandGroup struct {
	id       string
	commands []NamedCommand
}

func (d *ApplicationDescription) sortCommands(commands []NamedCommand) []commandGroup {
	var global []NamedCommand
	var namespaced []commandGroup
	index := map[string]int{}

	for _, nc := range commands {
		key := ExtractNamespace(nc.Name, 1)
		if key == "" || key == GlobalNamespace {
			global = append(global, nc)
		} else {
			i, ok := index[key]
			if !ok {
				i = len(namespaced)
				index[key] = i
				namespaced = append(namespaced, commandGroup{id: key})
			}
			namespaced[i].commands = append(namespaced[i].commands, nc)
		}
	}

	// ksort($commands) with SORT_REGULAR: names are array keys, so
	// canonical integer names compare as ints (PHP 8 rules).
	byName := func(a, b NamedCommand) int {
		return php.Compare(php.StrKey(a.Name).Value(), php.StrKey(b.Name).Value())
	}

	var sorted []commandGroup
	if len(global) > 0 {
		slices.SortStableFunc(global, byName)
		sorted = append(sorted, commandGroup{id: GlobalNamespace, commands: global})
	}

	if len(namespaced) > 0 {
		// ksort($namespacedCommands, SORT_STRING)
		slices.SortStableFunc(namespaced, func(a, b commandGroup) int { return strings.Compare(a.id, b.id) })
		for _, g := range namespaced {
			slices.SortStableFunc(g.commands, byName)
			sorted = append(sorted, g)
		}
	}

	return sorted
}

// formatDefaultValueJSON is TextDescriptor::formatDefaultValue().
func formatDefaultValueJSON(def any) string {
	if f, ok := def.(float64); ok && math.IsInf(f, 1) {
		return "INF"
	}

	switch v := def.(type) {
	case string:
		def = Escape(v)
	case []string:
		esc := make([]string, len(v))
		for i, s := range v {
			esc[i] = Escape(s)
		}
		def = esc
	case []any:
		esc := make([]any, len(v))
		for i, s := range v {
			if str, ok := s.(string); ok {
				esc[i] = Escape(str)
			} else {
				esc[i] = s
			}
		}
		def = esc
	}

	return strings.ReplaceAll(jsonEncodeValue(def), `\\`, `\`)
}

// isCommandKey is "$command->getName() === $name" for a name taken from an
// array key: a canonical integer name is an int key and never identical to
// the command's (string) name.
func isCommandKey(commandName, key string) bool {
	return commandName == key && php.StrKey(key).IsString()
}
