#!/usr/bin/env python3
"""Export the ML models needed by the Go pipeline to ONNX.

Outputs:
  {models}/panns_cnn14.onnx       — CNN14 SED, input (1, 64, 501), output (1, 527)
  {models}/florence2_base.onnx    — Florence-2 vision encoder (best-effort)
  {models}/florence2_decoder.onnx — Florence-2 text decoder (best-effort)
  {models}/class_labels_indices.csv — AudioSet label map (copied from panns-inference)

Usage:
    pip install torch panns-inference transformers onnx
    python scripts/export_onnx.py --models ./models

Florence-2 export notes:
  - The HF checkpoint ships custom modelling code; `trust_remote_code=True`
    is required on load.
  - The decoder is autoregressive; exporting it cleanly requires a single
    decoding step wrapper because torch.onnx.export can't trace a loop.
  - If transformers' built-in ONNX export supports the model, prefer that
    over the manual torch.onnx.export path below.
"""

from __future__ import annotations

import argparse
import shutil
import sys
from pathlib import Path


def export_panns(models_dir: Path) -> None:
    import torch
    from panns_inference import SoundEventDetection

    out_path = models_dir / "panns_cnn14.onnx"
    print(f"[panns] exporting to {out_path}")

    sed = SoundEventDetection(device="cpu")
    # `sed.model` is the Cnn14 SED torch module. Its forward expects a waveform
    # tensor; we wrap it to accept a pre-computed log-mel (1, 64, T) so the Go
    # side can do the STFT in pure Go.
    core = sed.model

    class MelWrapper(torch.nn.Module):
        def __init__(self, core):
            super().__init__()
            self.core = core

        def forward(self, mel: torch.Tensor) -> torch.Tensor:
            # panns Cnn14 has `bn0` + conv stack expecting (B, 1, T, 64). We
            # reshape from (B, 64, T) → (B, 1, T, 64).
            x = mel.transpose(1, 2).unsqueeze(1)
            x = self.core.bn0(x)
            x = self.core.conv_block1(x, pool_size=(2, 2), pool_type='avg')
            x = self.core.conv_block2(x, pool_size=(2, 2), pool_type='avg')
            x = self.core.conv_block3(x, pool_size=(2, 2), pool_type='avg')
            x = self.core.conv_block4(x, pool_size=(2, 2), pool_type='avg')
            x = self.core.conv_block5(x, pool_size=(2, 2), pool_type='avg')
            x = self.core.conv_block6(x, pool_size=(1, 1), pool_type='avg')
            x = torch.mean(x, dim=3)
            x1 = torch.max(x, dim=2)[0]
            x2 = torch.mean(x, dim=2)
            x = x1 + x2
            x = torch.sigmoid(self.core.fc_audioset(x))
            return x

    wrapper = MelWrapper(core).eval()
    dummy = torch.randn(1, 64, 501)
    torch.onnx.export(
        wrapper, dummy, str(out_path),
        input_names=["input"], output_names=["output"],
        dynamic_axes={"input": {2: "time"}, "output": {0: "batch"}},
        opset_version=17,
    )
    print(f"[panns] wrote {out_path}")


def export_labels(models_dir: Path) -> None:
    """Copy panns-inference's class_labels_indices.csv next to the model."""
    try:
        import panns_inference
    except ImportError:
        print("[labels] panns-inference not installed; skipping CSV copy", file=sys.stderr)
        return
    pkg_dir = Path(panns_inference.__file__).parent
    candidate = pkg_dir / "class_labels_indices.csv"
    if not candidate.exists():
        # newer versions bundle under a metadata folder
        matches = list(pkg_dir.rglob("class_labels_indices.csv"))
        if not matches:
            print("[labels] class_labels_indices.csv not found in panns-inference", file=sys.stderr)
            return
        candidate = matches[0]
    dst = models_dir / "class_labels_indices.csv"
    shutil.copyfile(candidate, dst)
    print(f"[labels] copied {candidate} -> {dst}")


def export_florence(models_dir: Path) -> None:
    """Best-effort Florence-2 export. Known to be finicky — see module docstring."""
    base_out = models_dir / "florence2_base.onnx"
    dec_out = models_dir / "florence2_decoder.onnx"
    print(f"[florence] attempting export to {base_out}")

    try:
        import torch
        from transformers import AutoModelForCausalLM, AutoProcessor
    except ImportError as e:
        print(f"[florence] missing deps ({e}); skipping", file=sys.stderr)
        return

    model_id = "microsoft/Florence-2-base"
    try:
        processor = AutoProcessor.from_pretrained(model_id, trust_remote_code=True)
        model = AutoModelForCausalLM.from_pretrained(
            model_id, trust_remote_code=True, torch_dtype=torch.float32
        ).eval()
    except Exception as e:
        print(f"[florence] failed to load model: {e}", file=sys.stderr)
        return

    # Vision encoder — takes pixel_values (1, 3, 768, 768) → image_features.
    class VisionWrapper(torch.nn.Module):
        def __init__(self, m):
            super().__init__()
            self.m = m

        def forward(self, pixel_values):
            return self.m._encode_image(pixel_values)

    try:
        dummy_pixels = torch.randn(1, 3, 768, 768)
        torch.onnx.export(
            VisionWrapper(model), dummy_pixels, str(base_out),
            input_names=["pixel_values"], output_names=["image_features"],
            dynamic_axes={"pixel_values": {0: "batch"}},
            opset_version=17,
        )
        print(f"[florence] wrote {base_out}")
    except Exception as e:
        print(f"[florence] encoder export failed: {e}", file=sys.stderr)

    # Decoder export is the hard part — a single-step wrapper around
    # model.language_model that consumes (encoder_hidden_states, decoder_input_ids)
    # and returns next-token logits. Left as a TODO; Go side tolerates a
    # missing decoder by writing bare vision_frame rows.
    print(f"[florence] decoder export skipped — complicated autoregressive trace; "
          f"Go vision stage will insert bare frame rows until {dec_out} exists.",
          file=sys.stderr)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--models", required=True, help="output dir for ONNX files")
    ap.add_argument("--skip-panns", action="store_true")
    ap.add_argument("--skip-florence", action="store_true")
    args = ap.parse_args()

    models_dir = Path(args.models).resolve()
    models_dir.mkdir(parents=True, exist_ok=True)

    if not args.skip_panns:
        export_panns(models_dir)
        export_labels(models_dir)
    if not args.skip_florence:
        export_florence(models_dir)

    return 0


if __name__ == "__main__":
    sys.exit(main())
