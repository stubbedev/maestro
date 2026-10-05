// Temporary stand-ins so the package compiles while the remaining ports are
// written. Each block is deleted by the port that replaces it.

package console

// --- replaced by symfony_style.go / question_helper.go ---

// SymfonyStyle stand-in.
type SymfonyStyle struct{}

// NewSymfonyStyle stand-in.
func NewSymfonyStyle(Input, Output) *SymfonyStyle { return &SymfonyStyle{} }

// Confirm stand-in.
func (*SymfonyStyle) Confirm(string, bool) (bool, error) { return false, nil }

// QuestionHelper stand-in.
type QuestionHelper struct{ HelperBase }

// NewQuestionHelper stand-in.
func NewQuestionHelper() *QuestionHelper { return &QuestionHelper{} }

// Name implements Helper.
func (*QuestionHelper) Name() string { return "question" }

// --- replaced by command_complete.go ---

// NewCompleteCommand stand-in.
func NewCompleteCommand() Commander {
	return NewCommand("_complete").SetHidden(true)
}

// NewDumpCompletionCommand stand-in.
func NewDumpCompletionCommand() Commander { return NewCommand("completion") }

// --- replaced by descriptor_json.go, descriptor_xml.go, descriptor_markdown.go ---

// JSONDescriptor stand-in.
type JSONDescriptor struct{ descriptorBase }

// Describe implements Descriptor.
func (*JSONDescriptor) Describe(Output, any, DescriptorOptions) error { return nil }

// XMLDescriptor stand-in.
type XMLDescriptor struct{ descriptorBase }

// Describe implements Descriptor.
func (*XMLDescriptor) Describe(Output, any, DescriptorOptions) error { return nil }

// MarkdownDescriptor stand-in.
type MarkdownDescriptor struct{ descriptorBase }

// Describe implements Descriptor.
func (*MarkdownDescriptor) Describe(Output, any, DescriptorOptions) error { return nil }
