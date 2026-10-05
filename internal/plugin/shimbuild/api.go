// The presence-parity data (docs/PLUGINS.md D7, §9.2): what
// tools/shimgen/reflect.php reports about Composer's classes.

package shimbuild

import (
	"encoding/json"
	"maps"
	"slices"
)

// API is the reflection of a set of classes, by class name.
type API struct {
	Classes map[string]*Class
}

// Class is one reflected class, interface or trait.
type Class struct {
	Kind             string             `json:"kind"`
	Abstract         bool               `json:"abstract"`
	Final            bool               `json:"final"`
	Parent           *string            `json:"parent"`
	Interfaces       []string           `json:"interfaces"`
	DirectInterfaces []string           `json:"directInterfaces"`
	Traits           []string           `json:"traits"`
	Constants        Members[*Constant] `json:"constants"`
	Properties       Members[*Property] `json:"properties"`
	Methods          Members[*Method]   `json:"methods"`
}

// Constant is a public or protected class constant.
type Constant struct {
	Visibility string `json:"visibility"`
	Value      string `json:"value"`
}

// Property is a public or protected property; Default is PHP source.
type Property struct {
	Visibility string  `json:"visibility"`
	Static     bool    `json:"static"`
	Type       *string `json:"type"`
	Default    *string `json:"default"`
}

// Method is a public or protected method the class declares.
type Method struct {
	Visibility string   `json:"visibility"`
	Static     bool     `json:"static"`
	Abstract   bool     `json:"abstract"`
	Final      bool     `json:"final"`
	ByRef      bool     `json:"byRef"`
	ReturnType *string  `json:"returnType"`
	Attributes []string `json:"attributes"`
	Params     []Param  `json:"params"`
}

// Param is a method parameter; Type and Default are PHP source.
type Param struct {
	Name     string  `json:"name"`
	Type     *string `json:"type"`
	ByRef    bool    `json:"byRef"`
	Variadic bool    `json:"variadic"`
	Optional bool    `json:"optional"`
	Default  *string `json:"default"`
}

// Members is a name-keyed set of members, iterated in name order (the
// order reflect.php writes them in).
type Members[V any] struct {
	Keys   []string
	Values map[string]V
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *Members[V]) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &m.Values); err != nil {
		return err
	}
	m.Keys = slices.Sorted(maps.Keys(m.Values))

	return nil
}

// ParseAPI parses reflect.php's output.
func ParseAPI(data []byte) (*API, error) {
	api := &API{}
	if err := json.Unmarshal(data, &api.Classes); err != nil {
		return nil, err
	}

	return api, nil
}

// Names returns the class names, sorted.
func (a *API) Names() []string { return slices.Sorted(maps.Keys(a.Classes)) }
