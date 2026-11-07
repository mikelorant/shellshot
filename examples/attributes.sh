#!/bin/sh
# shellcheck disable=SC1091,SC2034
# attributes.sh: text attributes: bold, faint, italic, underline styles (single,
# double, curly, dotted, dashed, coloured), strikethrough, overline, inverse,
# hidden and blink. Safe: prints escape sequences only. See examples/README.md.
#   sh examples/attributes.sh [--force] [--pause]

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='Text attributes: bold, faint, italic, underline styles, strikethrough, inverse, hidden.'
. "$EX_DIR/lib.sh"
ex_init "$@"

# attr LABEL SGR-PARAMS TEXT: one table row.
attr() {
    printf '  \033[2m%-22s\033[0m \033[%sm%s\033[0m\n' "$1" "$2" "$3"
    nap 150
}

ex_heading "attributes.sh: text attributes"
nap 300
attr 'normal' 0 'The quick brown fox jumps over the lazy dog'
attr 'bold (1)' 1 'The quick brown fox jumps over the lazy dog'
attr 'faint (2)' 2 'The quick brown fox jumps over the lazy dog'
attr 'italic (3)' 3 'The quick brown fox jumps over the lazy dog'
attr 'underline (4)' 4 'The quick brown fox jumps over the lazy dog'
attr 'double underline (21)' 21 'The quick brown fox jumps over the lazy dog'
attr 'curly (4:3)' 4:3 'The quick brown fox jumps over the lazy dog'
attr 'dotted (4:4)' 4:4 'The quick brown fox jumps over the lazy dog'
attr 'dashed (4:5)' 4:5 'The quick brown fox jumps over the lazy dog'
attr 'coloured curly (58)' '4:3;58;2;255;80;80' 'The quick brown fox jumps over the lazy dog'
attr 'strikethrough (9)' 9 'The quick brown fox jumps over the lazy dog'
attr 'overline (53)' 53 'The quick brown fox jumps over the lazy dog'
attr 'inverse (7)' 7 'The quick brown fox jumps over the lazy dog'
attr 'blink (5)' 5 'The quick brown fox jumps over the lazy dog'
printf '  \033[2m%-22s\033[0m [\033[8mthis text is hidden\033[0m] <- invisible between the brackets\n' 'hidden (8)'
nap 400

printf '\n'
ex_heading "combinations"
printf '  \033[1;3mbold italic\033[0m  \033[1;4mbold underline\033[0m  \033[3;9mitalic strike\033[0m  \033[1;3;4;9mall four\033[0m\n'
printf '  \033[1;31mbold red\033[0m  \033[2;32mfaint green\033[0m  \033[3;38;5;208morange italic\033[0m  \033[7;34minverse blue\033[0m\n'
printf '  \033[38;2;255;215;0;48;2;40;40;90m gold on navy \033[0m  \033[1;38;2;255;255;255;48;2;200;30;90m white on crimson \033[0m  \033[4:3;58;5;82;38;5;82m green wave \033[0m\n'
nap 400
printf '  mixing: normal \033[1mbold \033[3mbold-italic \033[22mitalic \033[23mnormal \033[4munder \033[1mbold-under \033[0mreset\n'
printf '  \033[7m inverse bar \033[0m  \033[2;7m faint inverse \033[0m  \033[1;7;32m bold green inverse \033[0m\n'
nap 1500
ex_finish
