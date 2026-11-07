#!/bin/sh
# shellcheck disable=SC1091,SC2034
# emoji.sh: emoji: single code points, text vs emoji presentation, skin tones,
# ZWJ sequences (family, profession, flag), regional-indicator flags and keycaps.
# Safe: prints text only. See contrib/examples/README.md.
#   sh contrib/examples/emoji.sh [--force] [--pause]

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='Emoji: singles, skin tones, ZWJ families, flags, keycaps.'
. "$EX_DIR/lib.sh"
ex_init "$@"

ZWJ=$(printf '\342\200\215')       # U+200D zero width joiner
VS16=$(printf '\357\270\217')      # U+FE0F emoji presentation selector
KEYCAP=$(printf '\342\203\243')    # U+20E3 combining enclosing keycap

# row LABEL ITEM...: label, then every item followed by a bar so misalignment shows.
row() {
    printf '\033[2m%-14s\033[0m' "$1"
    shift
    for item in "$@"; do
        printf '%s│' "$item"
    done
    printf '\n'
    nap 450
}

ex_heading "emoji.sh: emoji"
nap 300
row 'single' 😀 😎 🥳 🚀 🔥 🎉 🐙 🍕 🌈 💡 🎸 🦀 🐧 🐢 🌍 ⚡ ☕
row 'text | emoji' "❤" "❤$VS16" "☀" "☀$VS16" "✈" "✈$VS16" "✔" "✔$VS16" "☎" "☎$VS16"
row 'skin tones' 👍 👍🏻 👍🏼 👍🏽 👍🏾 👍🏿 👋🏽 🙌🏾 💪🏿 🤝🏻
row 'ZWJ family' "👨${ZWJ}👩${ZWJ}👧${ZWJ}👦" "👩${ZWJ}👩${ZWJ}👧" "👨${ZWJ}👨${ZWJ}👦${ZWJ}👦" "🧑${ZWJ}🧑${ZWJ}🧒"
row 'ZWJ people' "👩${ZWJ}💻" "🧑${ZWJ}🚀" "👩🏽${ZWJ}🚒" "👨🏿${ZWJ}🎤" "🧑🏻${ZWJ}🍳" "👨${ZWJ}🔬"
row 'ZWJ other' "🏳${VS16}${ZWJ}🌈" "🏴${ZWJ}☠${VS16}" "🐻${ZWJ}❄${VS16}" "🐈${ZWJ}⬛" "❤${VS16}${ZWJ}🔥" "👁${VS16}${ZWJ}🗨${VS16}"
row 'flags' 🇯🇵 🇫🇷 🇧🇷 🇨🇦 🇺🇸 🇬🇧 🇩🇪 🇮🇳 🇦🇺 🇰🇷 🇪🇸 🇮🇹 🇳🇱 🇸🇪
row 'other flags' 🏁 🚩 🏴 🏳
row 'keycaps' "0${VS16}${KEYCAP}" "1${VS16}${KEYCAP}" "2${VS16}${KEYCAP}" "3${VS16}${KEYCAP}" "4${VS16}${KEYCAP}" "5${VS16}${KEYCAP}" "6${VS16}${KEYCAP}" "7${VS16}${KEYCAP}" "8${VS16}${KEYCAP}" "9${VS16}${KEYCAP}" "#${VS16}${KEYCAP}" "*${VS16}${KEYCAP}" 🔟
row 'symbols' ✨ ⭐ ✅ ❌ ❓ ⚠ "⚠$VS16" ➡ "➡$VS16" "⬆$VS16" "↩$VS16" "▶$VS16" ⏩ ⏰ ⌛

printf '\n\033[2min a sentence:\033[0m shipping 🚀 with 💖 by 👩%s💻 at 🏳%s%s🌈 parties 🎉 in 🇯🇵 and 🇧🇷.\n' "$ZWJ" "$VS16" "$ZWJ"
printf '\033[2min bold/colour:\033[0m \033[1;31m🍎 apple\033[0m \033[1;32m🥝 kiwi\033[0m \033[1;33m🍋 lemon\033[0m \033[7m 🍇 inverse \033[0m \033[4m🫐 under\033[0m\n'
nap 1500
ex_finish
