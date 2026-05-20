package cli

import (
	"fmt"

	"github.com/MaikuMori/dfc/internal/core"
)

// openCore opens a Core for a single CLI invocation. The caller defers
// Close. Keeping per-command opens (rather than wiring a shared Core
// through kong) keeps each command's lifecycle obvious and matches the
// short-lived CLI process model.
func openCore() (*core.Core, error) {
	cr, err := core.Open(core.Options{})
	if err != nil {
		return nil, fmt.Errorf("could not open dfc data directory: %w", err)
	}
	return cr, nil
}
