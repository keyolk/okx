package cli

import (
	"context"

	okxapp "github.com/keyolk/okx/internal/app"
	"github.com/keyolk/okx/internal/tui"
)

func runTUI(ctx context.Context) error {
	c, err := okxapp.Open(ctx, okxapp.Options{
		ConfigPath: flagConfig,
		Refresh:    flagRefresh,
		TTL:        flagTTL,
		AllowStale: true,
		DeferFetch: true,
		Quiet:      true,
	})
	if err != nil {
		return err
	}
	return tui.Run(ctx, c)
}
