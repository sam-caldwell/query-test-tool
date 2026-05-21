// Package dialect provides the abstraction layer for multi-database support.
// Each supported database dialect registers a parser and weights file.
// Adding a new dialect requires: a weights JSON file, a parser implementation,
// and a registration call.
package dialect

import "fmt"

// Dialect identifies a supported database type.
type Dialect string

const (
	PostgreSQL Dialect = "postgresql"
	MySQL      Dialect = "mysql"
)

// Valid returns true if the dialect is a known, registered type.
func (d Dialect) Valid() bool {
	_, ok := registry[d]
	return ok
}

// String returns the dialect name.
func (d Dialect) String() string { return string(d) }

// Registration bundles a parser and weight data for a dialect.
type Registration struct {
	Name        Dialect
	Description string
	WeightsData []byte // raw JSON bytes (embedded at build time)
}

var registry = map[Dialect]*Registration{}

// Register adds a dialect to the global registry.
func Register(r *Registration) {
	registry[r.Name] = r
}

// Get returns the registration for a dialect, or an error if unsupported.
func Get(d Dialect) (*Registration, error) {
	r, ok := registry[d]
	if !ok {
		return nil, fmt.Errorf("unsupported dialect: %q (supported: %v)", d, Supported())
	}
	return r, nil
}

// Supported returns all registered dialect names.
func Supported() []Dialect {
	out := make([]Dialect, 0, len(registry))
	for d := range registry {
		out = append(out, d)
	}
	return out
}
