#!/bin/zsh
# Renders the artwork in design/ into resources/icon.png, the 1024px Dock icon, and cmd/mux/icons/app.png,
# the 256px icon shown inside the app. Needs ImageMagick.
#
# The artwork is a sheep peeking from the lower-left corner of a flat, cool-gray background. The
# background is keyed out and replaced with rebase's warm dark tile, and the tile follows the macOS
# icon grid: an 824px rounded square (radius 185) centered on the 1024 canvas, with a drop shadow.
# The in-app icon is the same tile without the margin and shadow, which would show as gray corners
# over the UI.
#
# Usage: scripts/make-icon.sh [artwork.png] [out.png] [app.png]
set -euo pipefail
cd "${0:A:h}/.."

src=${1:-"design/Kawaii Sheep Peeking from the Corner.png"}
out=${2:-resources/icon.png}
app=${3:-cmd/mux/icons/app.png}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The tile's gradient, top to bottom, and the face's color.
top='#36322E' bottom='#171513' face='#FEF5ED'

# The rounded corner of the tile cuts through the fur at the face's lower-left edge and leaves a
# sliver of its outline, so the fur below that edge takes the face's color. The polygon is in the
# artwork's 1254px coordinates.
magick "$src" -resize 1254x1254 -fill $face -draw "polygon 0,985 300,1285 0,1285" $work/art.png

# The background is flat, so a flood fill from the three corners it reaches keys it out. It is
# done at full size so the sheep's antialiased edge survives the scaling.
magick $work/art.png -alpha set -fuzz 6% -fill none \
  -draw "color 0,0 floodfill" -draw "color 1253,0 floodfill" -draw "color 1253,600 floodfill" \
  -resize 824x824 $work/sheep.png

magick -size 1024x1024 xc:none -fill white -draw "roundrectangle 100,100 923,923 185,185" $work/mask.png
magick -size 824x824 gradient:"$top-$bottom" $work/sheep.png -composite \
  -background none -gravity center -extent 1024x1024 \
  $work/mask.png -compose DstIn -composite $work/tile.png

magick $work/tile.png -crop 824x824+100+100 +repage -resize 256x256 -depth 8 $app

magick $work/tile.png \( +clone -background black -shadow 14x36+0+26 \) +swap \
  -background none -layers merge +repage -gravity center -crop 1024x1024+0+0 +repage -depth 8 $out
echo $out $app
