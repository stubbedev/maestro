// Package spdx ports composer/spdx-licenses 1.6.0 (src/SpdxLicenses.php)
// with its res/*.json data.
package spdx

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// The SPDX license and exception lists, as shipped in res/.
var (
	//go:embed res/spdx-licenses.json
	licensesJSON []byte
	//go:embed res/spdx-exceptions.json
	exceptionsJSON []byte
)

// License is one entry of the license list.
type License struct {
	Identifier  string
	Name        string
	OSIApproved bool
	Deprecated  bool
}

// LicenseInfo is what GetLicenseByIdentifier returns.
type LicenseInfo struct {
	Name        string
	OSIApproved bool
	URL         string
	Deprecated  bool
}

// Exception is one entry of the license exception list.
type Exception struct {
	Identifier string
	Name       string
}

// ExceptionInfo is what GetExceptionByIdentifier returns.
type ExceptionInfo struct {
	Name string
	URL  string
}

// SpdxLicenses ports Composer\Spdx\SpdxLicenses. It is immutable and safe
// for concurrent use.
type SpdxLicenses struct { //nolint:revive // The PHP class name.
	licenses     []License      // in file order
	licenseKeys  map[string]int // lowercased identifier => index
	exceptions   []Exception
	exceptionIDs map[string]int
	licenseTrie  *trie
	exceptTrie   *trie
}

var shared = sync.OnceValue(func() *SpdxLicenses {
	s, err := load(licensesJSON, exceptionsJSON)
	if err != nil {
		panic("spdx: embedded data: " + err.Error())
	}

	return s
})

// New returns the license database; the embedded data is parsed once.
func New() *SpdxLicenses {
	return shared()
}

func load(licensesData, exceptionsData []byte) (*SpdxLicenses, error) {
	s := &SpdxLicenses{
		licenseKeys:  map[string]int{},
		exceptionIDs: map[string]int{},
		licenseTrie:  &trie{},
		exceptTrie:   &trie{},
	}

	err := decodeOrdered(licensesData, func(id string, raw json.RawMessage) error {
		var fields struct {
			Name       string
			OSI        bool
			Deprecated bool
		}

		var tuple []json.RawMessage
		if err := json.Unmarshal(raw, &tuple); err != nil || len(tuple) != 3 {
			return fmt.Errorf("license %s: want [name, osi, deprecated]", id)
		}

		for i, dst := range []any{&fields.Name, &fields.OSI, &fields.Deprecated} {
			if err := json.Unmarshal(tuple[i], dst); err != nil {
				return fmt.Errorf("license %s: %w", id, err)
			}
		}

		key := strtolower(id)
		if i, ok := s.licenseKeys[key]; ok {
			s.licenses[i] = License{id, fields.Name, fields.OSI, fields.Deprecated}

			return nil
		}

		s.licenseKeys[key] = len(s.licenses)
		s.licenses = append(s.licenses, License{id, fields.Name, fields.OSI, fields.Deprecated})
		s.licenseTrie.add(key)

		return nil
	})
	if err != nil {
		return nil, err
	}

	err = decodeOrdered(exceptionsData, func(id string, raw json.RawMessage) error {
		var tuple []string
		if err := json.Unmarshal(raw, &tuple); err != nil || len(tuple) != 1 {
			return fmt.Errorf("exception %s: want [name]", id)
		}

		key := strtolower(id)
		if i, ok := s.exceptionIDs[key]; ok {
			s.exceptions[i] = Exception{id, tuple[0]}

			return nil
		}

		s.exceptionIDs[key] = len(s.exceptions)
		s.exceptions = append(s.exceptions, Exception{id, tuple[0]})
		s.exceptTrie.add(key)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return s, nil
}

// decodeOrdered calls f for each member of a JSON object, in order.
func decodeOrdered(data []byte, f func(key string, value json.RawMessage) error) error {
	dec := json.NewDecoder(bytes.NewReader(data))

	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("want a JSON object: %v", err)
	}

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}

		key, _ := tok.(string)

		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return err
		}

		if err := f(key, value); err != nil {
			return err
		}
	}

	return nil
}

// strtolower is PHP 8's strtolower: ASCII only.
func strtolower(s string) string {
	for i := range len(s) {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}

			return string(b)
		}
	}

	return s
}

// GetLicenseByIdentifier ports SpdxLicenses::getLicenseByIdentifier.
func (s *SpdxLicenses) GetLicenseByIdentifier(identifier string) (LicenseInfo, bool) {
	i, ok := s.licenseKeys[strtolower(identifier)]
	if !ok {
		return LicenseInfo{}, false
	}

	l := s.licenses[i]

	return LicenseInfo{
		Name:        l.Name,
		OSIApproved: l.OSIApproved,
		URL:         "https://spdx.org/licenses/" + l.Identifier + ".html#licenseText",
		Deprecated:  l.Deprecated,
	}, true
}

// GetLicenses ports SpdxLicenses::getLicenses: all licenses in file order
// (PHP keys them by lowercased identifier). The slice must not be modified.
func (s *SpdxLicenses) GetLicenses() []License {
	return s.licenses
}

// GetLicense returns the license of a (lowercased) key of GetLicenses.
func (s *SpdxLicenses) GetLicense(key string) (License, bool) {
	i, ok := s.licenseKeys[key]
	if !ok {
		return License{}, false
	}

	return s.licenses[i], true
}

// GetExceptionByIdentifier ports SpdxLicenses::getExceptionByIdentifier.
func (s *SpdxLicenses) GetExceptionByIdentifier(identifier string) (ExceptionInfo, bool) {
	i, ok := s.exceptionIDs[strtolower(identifier)]
	if !ok {
		return ExceptionInfo{}, false
	}

	e := s.exceptions[i]

	return ExceptionInfo{
		Name: e.Name,
		URL:  "https://spdx.org/licenses/" + e.Identifier + ".html#licenseExceptionText",
	}, true
}

// GetIdentifierByName ports SpdxLicenses::getIdentifierByName: the
// identifier of the first license, then exception, with that full name.
func (s *SpdxLicenses) GetIdentifierByName(name string) (string, bool) {
	for _, l := range s.licenses {
		if l.Name == name {
			return l.Identifier, true
		}
	}

	for _, e := range s.exceptions {
		if e.Name == name {
			return e.Identifier, true
		}
	}

	return "", false
}

// IsOsiApprovedByIdentifier ports SpdxLicenses::isOsiApprovedByIdentifier;
// unknown identifiers are not approved.
func (s *SpdxLicenses) IsOsiApprovedByIdentifier(identifier string) bool {
	i, ok := s.licenseKeys[strtolower(identifier)]

	return ok && s.licenses[i].OSIApproved
}

// IsDeprecatedByIdentifier ports SpdxLicenses::isDeprecatedByIdentifier;
// unknown identifiers are not deprecated.
func (s *SpdxLicenses) IsDeprecatedByIdentifier(identifier string) bool {
	i, ok := s.licenseKeys[strtolower(identifier)]

	return ok && s.licenses[i].Deprecated
}

// ValidateList ports SpdxLicenses::validate for an array: several licenses
// are validated as a disjunction, none as the empty string.
func (s *SpdxLicenses) ValidateList(licenses []string) bool {
	switch len(licenses) {
	case 0:
		return s.Validate("")
	case 1:
		return s.Validate(licenses[0])
	default:
		return s.Validate("(" + strings.Join(licenses, " OR ") + ")")
	}
}

// Validate ports SpdxLicenses::validate for a string: whether license is a
// known identifier or a valid SPDX license expression.
func (s *SpdxLicenses) Validate(license string) bool {
	if _, ok := s.licenseKeys[strtolower(license)]; ok {
		return true
	}

	return s.isValidLicenseExpression(license)
}
