package storage

import "errors"

// Dialect is retained for source compatibility. New callers use GORM's
// configured driver; it is no longer supplied to Store.New.
type Dialect string

const (
	Postgres Dialect = "postgres"
	MySQL    Dialect = "mysql"
)

var ErrInvalidDialect = errors.New("chainindex: invalid storage dialect")

func (d Dialect) Validate() error {
	if d == Postgres || d == MySQL { return nil }
	return ErrInvalidDialect
}
