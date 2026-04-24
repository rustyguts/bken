//go:build !onnx

package vision

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rustyguts/bken/internal/config"
)

const installHint = "vision: built without onnx support; rebuild with `-tags onnx` after installing libonnxruntime.so. Florence-2 model files are optional — stage will insert bare frame rows if they're absent."

func Detect(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64, force bool) error {
	return fmt.Errorf("%s", installHint)
}

func DetectAll(ctx context.Context, cfg *config.Config, conn *sql.DB, force bool) error {
	return fmt.Errorf("%s", installHint)
}
