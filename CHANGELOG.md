# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- **Job Progress**: Added a `progress` column to the `job` table and integrated progress tracking into the `reprocess` pipeline.
- **Icons**: Integrated Lucide icons across the entire application with automatic re-initialization for Datastar-swapped content.
- **Enhanced Job Queue**: The job queue page now displays a progress bar for active jobs (between 0-100%) and provides more detailed timestamps (including seconds).
- **Hot Reloading**: Configured `air` for Go development. Added `.air.toml` and `dev` stages to `Dockerfile` and `Dockerfile.gpu`.
- **GPU Support**: Added `Dockerfile.gpu` with `whisper.cpp` and `onnxruntime` shared library builds.
- **Theme**: Switched UI to DaisyUI "night" theme.

### Changed
- **UI Architecture**: Fully migrated to the Go-native UI in `internal/web/ui`.
- **Docker Compose**: Updated `docker-compose.yml` and `docker-compose.gpu.yml` to support hot reloading and the new GPU build stage.
- **Pipeline Robustness**: Modified the `score` stage to succeed (with zero moments) instead of failing when no audio events or transcript segments are detected.

### Removed
- **Legacy UI**: Deleted the `app/` directory (Nuxt 4) as it has been replaced by the Go-native UI.
