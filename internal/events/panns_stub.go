//go:build !onnx

package events

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rustyguts/bken/internal/config"
)

const installHint = "events: built without onnx support; rebuild with `-tags onnx` after installing libonnxruntime.so and exporting the model via `python scripts/export_onnx.py --models ./models`"

func Detect(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64, force bool) error {
	return fmt.Errorf("%s", installHint)
}

func DetectAll(ctx context.Context, cfg *config.Config, conn *sql.DB, force bool) error {
	return fmt.Errorf("%s", installHint)
}
