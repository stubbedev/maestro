// Ports src/Descriptor/XmlDescriptor.php (symfony/console), with the subset
// of DOMDocument it uses: createElement/createTextNode/setAttribute, node
// import, and saveXML() with formatOutput, which libxml2 serializes as
// reproduced by xmlDocument.save.

package console

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/php"
)

// XMLDescriptor renders the xml format.
type XMLDescriptor struct {
	descriptorBase
}

// Describe implements Descriptor.
func (d *XMLDescriptor) Describe(out Output, object any, options DescriptorOptions) error {
	return d.describe(d, out, object, options)
}

// InputDefinitionDocument returns the <definition> document.
func (*XMLDescriptor) InputDefinitionDocument(definition *InputDefinition) *XMLNode {
	definitionXML := newXMLElement("definition")

	argumentsXML := definitionXML.appendElement("arguments")
	for _, argument := range definition.Arguments() {
		argumentsXML.appendChildren(inputArgumentDocument(argument))
	}

	optionsXML := definitionXML.appendElement("options")
	for _, option := range definition.Options() {
		optionsXML.appendChildren(inputOptionDocument(option))
	}

	return definitionXML
}

// CommandDocument returns the <command> document.
func (d *XMLDescriptor) CommandDocument(command Commander, short bool) *XMLNode {
	base := command.Base()
	commandXML := newXMLElement("command")

	commandXML.setAttribute("id", base.Name())
	commandXML.setAttribute("name", base.Name())
	commandXML.setAttribute("hidden", xmlBool(base.IsHidden()))

	usagesXML := commandXML.appendElement("usages")

	commandXML.appendElement("description").appendText(strings.ReplaceAll(base.Description(), "\n", "\n "))

	if short {
		for _, usage := range base.Aliases() {
			usagesXML.appendUsage(usage)
		}
	} else {
		_ = base.MergeApplicationDefinition(false)

		usagesXML.appendUsage(base.Synopsis(false))
		for _, usage := range base.Aliases() {
			usagesXML.appendUsage(usage)
		}
		for _, usage := range base.Usages() {
			usagesXML.appendUsage(usage)
		}

		commandXML.appendElement("help").appendText(strings.ReplaceAll(base.ProcessedHelp(), "\n", "\n "))

		commandXML.appendChildren(d.InputDefinitionDocument(base.Definition()).children)
	}

	return commandXML
}

// ApplicationDocument returns the <symfony> document.
func (d *XMLDescriptor) ApplicationDocument(app *Application, namespace string, short bool) (*XMLNode, error) {
	rootXML := newXMLElement("symfony")

	if app.Name() != "UNKNOWN" {
		rootXML.setAttribute("name", app.Name())
		if app.Version() != "UNKNOWN" {
			rootXML.setAttribute("version", app.Version())
		}
	}

	commandsXML := rootXML.appendElement("commands")

	description := NewApplicationDescription(app, namespace, true)

	hasNamespace := php.Truthy(namespace)
	if hasNamespace {
		commandsXML.setAttribute("namespace", namespace)
	}

	names, commands, err := description.Commands()
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		commandsXML.appendChildren([]*XMLNode{d.CommandDocument(commands[name], short)})
	}

	if !hasNamespace {
		namespacesXML := rootXML.appendElement("namespaces")

		namespaces, err := description.Namespaces()
		if err != nil {
			return nil, err
		}
		for _, ns := range namespaces {
			namespaceArrayXML := namespacesXML.appendElement("namespace")
			namespaceArrayXML.setAttribute("id", ns.ID)

			for _, name := range ns.Commands {
				namespaceArrayXML.appendElement("command").appendText(name)
			}
		}
	}

	return rootXML, nil
}

func (d *XMLDescriptor) describeInputArgument(argument *InputArgument, _ DescriptorOptions) {
	d.writeDocument(inputArgumentDocument(argument))
}

func (d *XMLDescriptor) describeInputOption(option *InputOption, _ DescriptorOptions) {
	d.writeDocument(inputOptionDocument(option))
}

func (d *XMLDescriptor) describeInputDefinition(definition *InputDefinition, _ DescriptorOptions) {
	d.writeDocument([]*XMLNode{d.InputDefinitionDocument(definition)})
}

func (d *XMLDescriptor) describeCommand(command Commander, options DescriptorOptions) {
	d.writeDocument([]*XMLNode{d.CommandDocument(command, options.Short)})
}

func (d *XMLDescriptor) describeApplication(app *Application, options DescriptorOptions) error {
	doc, err := d.ApplicationDocument(app, options.Namespace, options.Short)
	if err != nil {
		return err
	}
	d.writeDocument([]*XMLNode{doc})

	return nil
}

// writeDocument writes the document saved with formatOutput = true.
func (d *XMLDescriptor) writeDocument(nodes []*XMLNode) {
	d.write(saveXML(nodes), false)
}

func xmlBool(b bool) string {
	if b {
		return "1"
	}

	return "0"
}

// xmlDefaults is the $defaults list of getInputArgumentDocument and
// getInputOptionDocument: an array as is, a bool var_export()ed, any other
// truthy value as a single entry.
func xmlDefaults(def any) []string {
	switch v := def.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, len(v))
		for i, e := range v {
			out[i] = phpToString(e)
		}

		return out
	case bool:
		if v {
			return []string{"true"}
		}

		return []string{"false"}
	}
	if inputTruthy(def) {
		return []string{phpToString(def)}
	}

	return nil
}

func inputArgumentDocument(argument *InputArgument) []*XMLNode {
	objectXML := newXMLElement("argument")
	objectXML.setAttribute("name", argument.Name())
	objectXML.setAttribute("is_required", xmlBool(argument.IsRequired()))
	objectXML.setAttribute("is_array", xmlBool(argument.IsArray()))
	objectXML.appendElement("description").appendText(argument.Description())

	defaultsXML := objectXML.appendElement("defaults")
	for _, def := range xmlDefaults(argument.Default()) {
		defaultsXML.appendElement("default").appendText(def)
	}

	return []*XMLNode{objectXML}
}

func inputOptionDocument(option *InputOption) []*XMLNode {
	objectXML := newXMLElement("option")
	objectXML.setAttribute("name", "--"+option.Name())
	shortcut := option.Shortcut()
	if first, _, ok := strings.Cut(shortcut, "|"); ok {
		objectXML.setAttribute("shortcut", "-"+first)
		objectXML.setAttribute("shortcuts", "-"+strings.ReplaceAll(shortcut, "|", "|-"))
	} else if php.Truthy(shortcut) {
		objectXML.setAttribute("shortcut", "-"+shortcut)
	} else {
		objectXML.setAttribute("shortcut", "")
	}
	objectXML.setAttribute("accept_value", xmlBool(option.AcceptValue()))
	objectXML.setAttribute("is_value_required", xmlBool(option.IsValueRequired()))
	objectXML.setAttribute("is_multiple", xmlBool(option.IsArray()))
	objectXML.appendElement("description").appendText(option.Description())

	if option.AcceptValue() {
		defaults := xmlDefaults(option.Default())
		defaultsXML := objectXML.appendElement("defaults")
		for _, def := range defaults {
			defaultsXML.appendElement("default").appendText(def)
		}
	}

	nodes := []*XMLNode{objectXML}

	if option.IsNegatable() {
		negXML := newXMLElement("option")
		negXML.setAttribute("name", "--no-"+option.Name())
		negXML.setAttribute("shortcut", "")
		negXML.setAttribute("accept_value", "0")
		negXML.setAttribute("is_value_required", "0")
		negXML.setAttribute("is_multiple", "0")
		negXML.appendElement("description").appendText(`Negate the "--` + option.Name() + `" option`)
		nodes = append(nodes, negXML)
	}

	return nodes
}

// XMLNode is a DOM element or text node.
type XMLNode struct {
	name     string // element name; "" for a text or entity reference node
	text     string
	raw      bool // an entity reference node, written verbatim
	attrs    []xmlAttr
	children []*XMLNode
}

type xmlAttr struct{ name, value string }

func newXMLElement(name string) *XMLNode { return &XMLNode{name: name} }

// setAttribute replaces an existing attribute in place or appends it.
func (n *XMLNode) setAttribute(name, value string) {
	for i := range n.attrs {
		if n.attrs[i].name == name {
			n.attrs[i].value = value

			return
		}
	}
	n.attrs = append(n.attrs, xmlAttr{name, value})
}

func (n *XMLNode) appendElement(name string) *XMLNode {
	c := newXMLElement(name)
	n.children = append(n.children, c)

	return c
}

// appendUsage is appendChild(createElement('usage', $value)). libxml2
// parses the value for references: predefined entities and character
// references are resolved, other "&name;" references become entity
// reference nodes (written back verbatim), and malformed references are
// dropped the way libxml2 2.15 does.
func (n *XMLNode) appendUsage(value string) {
	c := n.appendElement("usage")
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			c.appendText(text.String())
			text.Reset()
		}
	}
	for i := 0; i < len(value); i++ {
		if value[i] != '&' {
			text.WriteByte(value[i])

			continue
		}
		if i+1 < len(value) && value[i+1] == '#' {
			// character reference: digits must be followed by ";"
			j, base := i+2, 10
			if j < len(value) && value[j] == 'x' {
				j, base = j+1, 16
			}
			k := j
			for k < len(value) && isRefDigit(value[k], base) {
				k++
			}
			if k == len(value) || value[k] != ';' {
				i = k - 1 // the malformed reference is dropped

				continue
			}
			if v, err := strconv.ParseUint(value[j:k], base, 21); err == nil {
				if r := rune(v); r != 0 && utf8.ValidRune(r) {
					text.WriteRune(r)
				}
			}
			i = k

			continue
		}
		end := strings.IndexByte(value[i+1:], ';')
		if end < 0 {
			continue // an unterminated reference loses its "&"
		}
		name := value[i+1 : i+1+end]
		i += 1 + end
		switch name {
		case "":
		case "lt":
			text.WriteByte('<')
		case "gt":
			text.WriteByte('>')
		case "amp":
			text.WriteByte('&')
		case "quot":
			text.WriteByte('"')
		case "apos":
			text.WriteByte('\'')
		default:
			flush()
			c.children = append(c.children, &XMLNode{text: "&" + name + ";", raw: true})
		}
	}
	flush()
}

func isRefDigit(c byte, base int) bool {
	if c >= '0' && c <= '9' {
		return true
	}

	return base == 16 && ((c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))
}

// appendText is appendChild(createTextNode($text)); an empty text node
// still counts as a child.
func (n *XMLNode) appendText(text string) {
	n.children = append(n.children, &XMLNode{text: text})
}

func (n *XMLNode) appendChildren(nodes []*XMLNode) {
	n.children = append(n.children, nodes...)
}

// saveXML is DOMDocument::saveXML() with formatOutput = true for a document
// encoded as UTF-8: the XML declaration, then each top-level node followed
// by a newline. libxml2 indents element children by two spaces per level,
// unless an element has a text child, in which case its whole content is
// written as is.
func saveXML(nodes []*XMLNode) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	for _, n := range nodes {
		n.dump(&b, 0, true)
		b.WriteByte('\n')
	}

	return b.String()
}

func (n *XMLNode) dump(b *strings.Builder, level int, format bool) {
	if n.raw {
		b.WriteString(n.text)

		return
	}
	if n.name == "" {
		writeXMLEscaped(b, n.text, false)

		return
	}

	b.WriteByte('<')
	b.WriteString(n.name)
	for _, a := range n.attrs {
		b.WriteByte(' ')
		b.WriteString(a.name)
		b.WriteString(`="`)
		writeXMLEscaped(b, a.value, true)
		b.WriteByte('"')
	}
	if len(n.children) == 0 {
		b.WriteString("/>")

		return
	}

	if format {
		for _, c := range n.children {
			if c.name == "" {
				format = false

				break
			}
		}
	}

	b.WriteByte('>')
	if format {
		b.WriteByte('\n')
	}
	for _, c := range n.children {
		if format && c.name != "" {
			writeXMLIndent(b, level+1)
		}
		c.dump(b, level+1, format)
		if format {
			b.WriteByte('\n')
		}
	}
	if format {
		writeXMLIndent(b, level)
	}
	b.WriteString("</")
	b.WriteString(n.name)
	b.WriteByte('>')
}

// writeXMLIndent writes libxml2's indentation, capped at 30 levels.
func writeXMLIndent(b *strings.Builder, level int) {
	b.WriteString(strings.Repeat("  ", min(level, 30)))
}

// writeXMLEscaped escapes text content like xmlEscapeContent (< > & and
// \r) or attribute values like xmlBufAttrSerializeTxtContent (also " \n
// and \t).
func writeXMLEscaped(b *strings.Builder, s string, attr bool) {
	for i := range len(s) {
		switch c := s[i]; c {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '\r':
			b.WriteString("&#13;")
		case '"':
			if attr {
				b.WriteString("&quot;")
			} else {
				b.WriteByte(c)
			}
		case '\n':
			if attr {
				b.WriteString("&#10;")
			} else {
				b.WriteByte(c)
			}
		case '\t':
			if attr {
				b.WriteString("&#9;")
			} else {
				b.WriteByte(c)
			}
		default:
			b.WriteByte(c)
		}
	}
}
