// Package cli wires Cobra subcommands. Each stage of the pipeline is its
// own subcommand; `serve` starts the Echo web server + asynq worker.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/rustyguts/bken/internal/config"
)

// appCtx is a tiny helper to attach loaded config to a subcommand.
type appCtx struct {
	cfg *config.Config
}

func newAppCtx() *appCtx {
	return &appCtx{cfg: config.Load()}
}

// Root returns the bken root command wired with all subcommands.
func Root() *cobra.Command {
	ctx := newAppCtx()

	root := &cobra.Command{
		Use:   "bken",
		Short: "Gaming video funny-moment extraction pipeline",
	}

	root.AddCommand(
		newInitDBCmd(ctx),
		newIngestCmd(ctx),
		newExtractAudioCmd(ctx),
		newTranscribeCmd(ctx),
		newDetectEventsCmd(ctx),
		newDetectVisionCmd(ctx),
		newScoreCmd(ctx),
		newRankCmd(ctx),
		newCreateClipCmd(ctx),
		newReprocessCmd(ctx),
		newLibraryCmd(ctx),
		newPipelineCmd(ctx),
		newServeCmd(ctx),
		newWorkerCmd(ctx),
	)

	return root
}
