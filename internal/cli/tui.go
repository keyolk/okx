package cli

import (
	"context"

	"github.com/keyolk/okx/internal/tui"
)

func runTUI(ctx context.Context) error {
	c, err := open(ctx)
	if err != nil {
		return err
	}
	return tui.Run(ctx, c)
}
