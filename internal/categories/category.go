package categories

import (
	"errors"

	"kakei/internal/core"
)

// Category is a label a transaction is filed under. It holds no money — what a
// category is worth is the sum of what points at it, which is the transactions
// module's job to work out.
type Category struct {
	ID          int64
	Code        string
	Name        string
	Description string
	Color       string
	CreatedAt   string
	UpdatedAt   string
}

func (c Category) Col() core.Color { return core.ColorByName(c.Color) }

var ErrNotFound = errors.New("category not found")
