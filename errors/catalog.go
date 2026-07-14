package errors

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Definition is one stable, transport-safe business error declaration.
type Definition struct {
	code       int
	name       string
	message    string
	httpStatus int
}

func Define(code int, name, message string, httpStatus int) Definition {
	return Definition{code: code, name: strings.TrimSpace(name), message: strings.TrimSpace(message), httpStatus: httpStatus}
}

func (d Definition) Code() int       { return d.code }
func (d Definition) Name() string    { return d.name }
func (d Definition) Message() string { return d.message }
func (d Definition) HTTPStatus() int { return d.httpStatus }
func (d Definition) New() *Error     { return New(d.code, d.message, WithHTTPStatus(d.httpStatus)) }
func (d Definition) Wrap(cause error) *Error {
	return Wrap(cause, d.code, d.message, WithHTTPStatus(d.httpStatus))
}

// Catalog validates and resolves a finite set of business error codes.
type Catalog struct {
	byCode map[int]Definition
	byName map[string]Definition
}

func NewCatalog(definitions ...Definition) (*Catalog, error) {
	catalog := &Catalog{byCode: make(map[int]Definition, len(definitions)), byName: make(map[string]Definition, len(definitions))}
	for _, definition := range definitions {
		if definition.code <= 0 || definition.code > CodeMax {
			return nil, fmt.Errorf("errors: invalid business code %d", definition.code)
		}
		if definition.name == "" {
			return nil, fmt.Errorf("errors: code %d has an empty name", definition.code)
		}
		if definition.message == "" {
			return nil, fmt.Errorf("errors: %s has an empty message", definition.name)
		}
		if definition.httpStatus < http.StatusBadRequest || definition.httpStatus > 599 {
			return nil, fmt.Errorf("errors: %s has invalid HTTP status %d", definition.name, definition.httpStatus)
		}
		if existing, ok := catalog.byCode[definition.code]; ok {
			return nil, fmt.Errorf("errors: duplicate code %d (%s and %s)", definition.code, existing.name, definition.name)
		}
		if existing, ok := catalog.byName[definition.name]; ok {
			return nil, fmt.Errorf("errors: duplicate name %s (codes %d and %d)", definition.name, existing.code, definition.code)
		}
		catalog.byCode[definition.code], catalog.byName[definition.name] = definition, definition
	}
	return catalog, nil
}

func MustCatalog(definitions ...Definition) *Catalog {
	catalog, err := NewCatalog(definitions...)
	if err != nil {
		panic(err)
	}
	return catalog
}

// MergeCatalogs builds one process catalog from service-local and shared
// catalogs. Duplicate codes or names across module/repository boundaries are
// rejected by the same rules as NewCatalog.
func MergeCatalogs(catalogs ...*Catalog) (*Catalog, error) {
	var definitions []Definition
	for _, catalog := range catalogs {
		definitions = append(definitions, catalog.Definitions()...)
	}
	return NewCatalog(definitions...)
}

func MustMergeCatalogs(catalogs ...*Catalog) *Catalog {
	catalog, err := MergeCatalogs(catalogs...)
	if err != nil {
		panic(err)
	}
	return catalog
}

// Definitions returns a stable copy suitable for governance tooling and
// composing catalogs supplied by shared Go modules.
func (c *Catalog) Definitions() []Definition {
	if c == nil {
		return nil
	}
	definitions := make([]Definition, 0, len(c.byCode))
	for _, definition := range c.byCode {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].code < definitions[j].code })
	return definitions
}

func (c *Catalog) LookupCode(code int) (Definition, bool) {
	if c == nil {
		return Definition{}, false
	}
	definition, ok := c.byCode[code]
	return definition, ok
}

func (c *Catalog) LookupName(name string) (Definition, bool) {
	if c == nil {
		return Definition{}, false
	}
	definition, ok := c.byName[strings.TrimSpace(name)]
	return definition, ok
}

// HTTPStatus returns 500 for unknown codes so unmapped downstream business
// failures never masquerade as a successful HTTP response.
func (c *Catalog) HTTPStatus(code int) int {
	if definition, ok := c.LookupCode(code); ok {
		return definition.httpStatus
	}
	return http.StatusInternalServerError
}

// FromCode converts a downstream RPC business code into an HTTP-aware Error.
// Unknown codes preserve their public code/message but deliberately map to 500.
func (c *Catalog) FromCode(code int, message string) *Error {
	if code == 0 {
		return nil
	}
	status := c.HTTPStatus(code)
	if definition, ok := c.LookupCode(code); ok && strings.TrimSpace(message) == "" {
		message = definition.message
	}
	if strings.TrimSpace(message) == "" {
		message = MessageInternal
	}
	return New(code, message, WithHTTPStatus(status))
}
