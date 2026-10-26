#!/bin/zsh
# Combines a light and a dark screenshot of the same window into docs/screenshot.png, the README's
# picture, split along its diagonal, shadow included: light above it, dark below. Needs ImageMagick.
#
# Take both without the window's shadow, with Cmd+Shift+4, then Space, then Option held while
# clicking the window, switching only the appearance in between, without moving or resizing the
# window. macOS shades a dark window's shadow darker than a light one's, which would show the
# diagonal through the shadow, so the script gives both the same shadow, as large as macOS's on a
# Retina screen, before it splits them.
#
# Usage: scripts/make-screenshot.sh light.png dark.png [out.png]
set -euo pipefail

light=$1 dark=$2
out=${3:-${0:A:h}/../docs/screenshot.png}

size=$(magick identify -format %wx%h "$light")
if [[ $size != $(magick identify -format %wx%h "$dark") ]]; then
  echo "the two screenshots differ in size" >&2
  exit 1
fi
# The shadow widens the picture by 112 pixels on either side and heightens it by 76 above and 148
# below.
w=$((${size%x*} + 224)) h=$((${size#*x} + 224))
shadow=(\( +clone -background black -shadow 45x56+0+36 \) +swap -background none -compose Over -layers merge +repage)

mkdir -p "${out:h}"
# The mask keeps the light one above the diagonal and the dark one below it.
mask=(-size ${w}x$h xc:none -fill white -draw "polygon 0,0 $w,0 0,$h")
magick \( "$light" $shadow \( $mask \) -compose DstIn -composite \) \
  \( "$dark" $shadow \( $mask \) -compose DstOut -composite \) \
  -compose Plus -composite -strip "$out"
echo $out
