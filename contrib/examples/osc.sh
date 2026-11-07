#!/bin/sh
# shellcheck disable=SC1091,SC2034,SC1003
# osc.sh: operating system commands: the window title (OSC 0 and 2) and clickable
# hyperlinks (OSC 8). Only prints; the links are text and are never opened.
# See contrib/examples/README.md.
#   sh contrib/examples/osc.sh [--force] [--pause]

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='OSC: window title and OSC 8 hyperlinks.'
. "$EX_DIR/lib.sh"
ex_init "$@"

# title TEXT: OSC 2 with the ST terminator.
title() { printf '\033]2;%s\033\\' "$1"; }
# link URL TEXT: an OSC 8 hyperlink.
link() { printf '\033]8;;%s\033\\%s\033]8;;\033\\' "$1" "$2"; }
# linkid ID URL TEXT: a hyperlink with an id, so a terminal can join split pieces.
linkid() { printf '\033]8;id=%s;%s\033\\%s\033]8;;\033\\' "$1" "$2" "$3"; }

ex_heading "osc.sh: window title and hyperlinks"
printf '\n'

# Remember the current title (XTWINOPS 22) so the exit trap can put it back (23).
printf '\033[22;2t'
EX_TITLE=1

printf '\033[1mWindow title\033[0m   OSC 2 ; text ST   (look at the title bar or tab)\n'
for step in 'shellshot: step 1 of 3' 'shellshot: step 2 of 3' 'shellshot: step 3 of 3'; do
    title "$step"
    printf '  title is now \033[7m %s \033[0m\n' "$step"
    nap 800
done
printf '\033]0;osc.sh: icon and title (OSC 0, BEL-terminated)\007'
printf '  OSC 0 sets both icon name and title, here ended with BEL\n'
nap 800
printf '\n'

printf '\033[1mHyperlinks\033[0m     OSC 8 ; params ; URI ST  text  OSC 8 ; ; ST\n'
printf '  project page:   '
link 'https://github.com/ghostty-org/ghostty' 'Ghostty'
printf '\n'
nap 400
printf '  styled link:    \033[4;38;2;90;170;255m'
link 'https://example.com/styled' 'underlined blue link'
printf '\033[0m\n'
nap 400
printf '  mail link:      '
link 'mailto:someone@example.com' 'someone@example.com'
printf '\n'
nap 400
printf '  in a sentence:  read the '
link 'https://example.com/docs' 'documentation'
printf ', then the '
link 'https://example.com/changelog' 'changelog'
printf ', then relax.\n'
nap 400
printf '  one link, two pieces (same id joins them):\n'
printf '    '
linkid demo 'https://example.com/long-link' 'first half of the link'
printf ' ... plain text between ... '
linkid demo 'https://example.com/long-link' 'second half'
printf '\n'
nap 400
printf '  \033[2m(links are only printed, never opened. Hover or ctrl-click in a terminal.)\033[0m\n'
nap 1200

printf '\n\033[1mBoth together\033[0m\n'
title 'shellshot: osc.sh done'
printf '  title set to "shellshot: osc.sh done", then restored when this script exits\n'
nap 1500
ex_finish
