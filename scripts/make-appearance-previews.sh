#!/bin/zsh
# Writes the settings' appearance choices, cmd/mux/icons/appearance-{light,dark,auto}.svg: the app's
# window drawn small, as System Settings draws macOS's, in Vellum's light and dark colors (theme.go),
# and for Auto the two split along the diagonal, light above it and dark below, as the README's
# screenshot is. The window shows the sidebar and three panes of a split tab, the last showing git
# log.
#
# Usage: scripts/make-appearance-previews.sh
set -euo pipefail
cd "${0:A:h}/.."

# The picture is 120 by 76, the window's corners rounded by 6.
w=120 h=76

# window writes the window in the colors set in the variables: bg the panes' ground, surface the
# sidebar's, chosen its chosen tab's, border the lines, text and muted the text, accent the
# controls' accent, and green and yellow the terminals' green, for the prompts, and yellow, for the
# commits' hashes.
window() {
  cat <<EOF
    <rect width="$w" height="$h" fill="$bg"/>
    <rect width="34" height="$h" fill="$surface"/>
    <rect x="34" width="0.5" height="$h" fill="$border"/>
    <rect x="77" width="0.5" height="$h" fill="$border"/>
    <rect x="77.5" y="40" width="42.5" height="0.5" fill="$border"/>
    <circle cx="6" cy="6" r="1.6" fill="#ff5f57"/>
    <circle cx="11" cy="6" r="1.6" fill="#febc2e"/>
    <circle cx="16" cy="6" r="1.6" fill="#28c840"/>
    <g fill="$muted">
      <rect x="5" y="13" width="8" height="1.6" rx="0.8"/>
      <rect x="5" y="35" width="9" height="1.6" rx="0.8"/>
      <rect x="5" y="49" width="10" height="1.6" rx="0.8"/>
    </g>
    <rect x="3" y="17" width="28" height="7" rx="2" fill="$chosen"/>
    <g fill="$accent">
      <rect x="5.5" y="19.7" width="3" height="1.6" rx="0.4"/>
      <rect x="5.5" y="27.7" width="3" height="1.6" rx="0.4"/>
      <rect x="5.5" y="40.7" width="3" height="1.6" rx="0.4"/>
      <rect x="5.5" y="54.7" width="3" height="1.6" rx="0.4"/>
      <rect x="5.5" y="61.7" width="3" height="1.6" rx="0.4"/>
    </g>
    <g fill="$text">
      <rect x="10.5" y="19.7" width="12" height="1.6" rx="0.8"/>
      <rect x="10.5" y="27.7" width="9" height="1.6" rx="0.8"/>
      <rect x="10.5" y="40.7" width="11" height="1.6" rx="0.8"/>
      <rect x="10.5" y="54.7" width="10" height="1.6" rx="0.8"/>
      <rect x="10.5" y="61.7" width="9" height="1.6" rx="0.8"/>
    </g>
    <g fill="$muted">
      <rect x="39" y="5.2" width="22" height="1.6" rx="0.8"/>
      <rect x="81" y="5.2" width="22" height="1.6" rx="0.8"/>
      <rect x="81" y="44.2" width="20" height="1.6" rx="0.8"/>
      <rect x="42" y="14" width="24" height="1.4" rx="0.7"/>
      <rect x="42" y="17" width="30" height="1.4" rx="0.7"/>
      <rect x="42" y="20" width="19" height="1.4" rx="0.7"/>
      <rect x="42" y="23" width="26" height="1.4" rx="0.7"/>
      <rect x="85" y="13" width="12" height="1.4" rx="0.7"/>
      <rect x="83" y="33" width="20" height="1.4" rx="0.7"/>
    </g>
    <g fill="$green">
      <rect x="39" y="11" width="1.5" height="1.4" rx="0.4"/>
      <rect x="39" y="28" width="1.5" height="1.4" rx="0.4"/>
    </g>
    <g fill="$text">
      <rect x="42" y="11" width="16" height="1.4" rx="0.7"/>
      <rect x="42" y="28" width="12" height="1.4" rx="0.7"/>
      <rect x="85" y="10" width="14" height="1.6" rx="0.8"/>
    </g>
    <g fill="$yellow">
EOF
  # git log: a hash, then a commit message as wide as given.
  local y=50 m
  for m in 22 26 18 24 20 27 16 23; do
    print "      <rect x=\"81\" y=\"$y\" width=\"6\" height=\"1.4\" rx=\"0.7\"/>"
    (( y += 3 ))
  done
  print '    </g>'
  print "    <g fill=\"$text\">"
  y=50
  for m in 22 26 18 24 20 27 16 23; do
    print "      <rect x=\"88.5\" y=\"$y\" width=\"$m\" height=\"1.4\" rx=\"0.7\"/>"
    (( y += 3 ))
  done
  print '    </g>'
  # The window's outline, inside its edge.
  print "    <rect x=\"0.25\" y=\"0.25\" width=\"$((w - 0.5))\" height=\"$((h - 0.5))\" rx=\"5.75\" fill=\"none\" stroke=\"$border\" stroke-width=\"0.5\"/>"
}

light() { bg='#faf8f5' surface='#efeae4' chosen='#e0d6cc' border='#cbbdae' text='#45413d' muted='#8a837b' accent='#b8622a' green='#507c39' yellow='#956900' window }
dark() { bg='#1a1816' surface='#151311' chosen='#413b35' border='#3e3934' text='#d8d3cb' muted='#7c7670' accent='#cf9370' green='#a6c485' yellow='#dbbc7f' window }

# svg writes a picture of what its arguments draw, clipped to the window's rounded corners.
svg() {
  print "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 $w $h\">"
  print '  <defs>'
  print "    <clipPath id=\"window\"><rect width=\"$w\" height=\"$h\" rx=\"6\"/></clipPath>"
  print "    <clipPath id=\"below\"><path d=\"M$w 0V${h}H0z\"/></clipPath>"
  print '  </defs>'
  "$@"
  print '</svg>'
}

icons=cmd/mux/icons
svg eval 'print "  <g clip-path=\"url(#window)\">"; light; print "  </g>"' > $icons/appearance-light.svg
svg eval 'print "  <g clip-path=\"url(#window)\">"; dark; print "  </g>"' > $icons/appearance-dark.svg
svg eval 'print "  <g clip-path=\"url(#window)\">"; light; print "  </g>"
  print "  <g clip-path=\"url(#below)\"><g clip-path=\"url(#window)\">"; dark; print "  </g></g>"' > $icons/appearance-auto.svg
echo $icons/appearance-{light,dark,auto}.svg
