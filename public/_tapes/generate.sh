#!/usr/bin/env bash
# Regenerate every screenshot in public/ from real ~/.claudeops data.
# Requires: go, vhs (charmbracelet/vhs), ttyd, ffmpeg, ImageMagick.
# Run from the repo root:  bash public/_tapes/generate.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

echo "==> building binary"
CGO_ENABLED=0 go build -o claudeops ./cmd/claudeops

echo "==> capturing tabs"
vhs public/_tapes/tabs.tape

echo "==> capturing features"
vhs public/_tapes/feature-help-day.tape
vhs public/_tapes/feature-task.tape
vhs public/_tapes/feature-session.tape

# The session detail view renders under two ttyd viewport-diff ghost bands
# (a stray browse breadcrumb near the top and the browse table header just
# below the summary card). Splice them out. Offsets are tuned for the
# 1300x1120 / FontSize 16 render above; re-check them if that changes.
echo "==> cleaning session-detail ghost bands"
src=public/13-session-detail.png
convert "$src" -crop 1300x48+0+0    +repage /tmp/_sd_a.png
convert "$src" -crop 1300x166+0+72  +repage /tmp/_sd_b.png
convert "$src" -crop 1300x851+0+269 +repage /tmp/_sd_c.png
convert /tmp/_sd_a.png /tmp/_sd_b.png /tmp/_sd_c.png -append "$src"
rm -f /tmp/_sd_a.png /tmp/_sd_b.png /tmp/_sd_c.png public/_tapes/.render.gif

echo "==> done. See public/README.md for the manifest and privacy notes."
