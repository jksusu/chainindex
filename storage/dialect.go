// Package storage contains database-specific configuration for chainindex.
package storage

import "errors"

var ErrInvalidDialect = errors.New("chainindex: invalid storage dialect")

// Dialect selects the SQL syntax used by the storage layer.
type Dialect string

const (
	Postgres Dialect = "postgres"
	MySQL    Dialect = "mysql"
)

// Validate reports whether d is a supported SQL dialect.
func (d Dialect) Validate() error {
	switch d {
	case Postgres, MySQL:
		return nil
	default:
		return ErrInvalidDialect
	}
}
