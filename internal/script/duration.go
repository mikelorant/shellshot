package script

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

// ParseDuration parses "<number><unit>" with unit ms, s or m ("500ms",
// "1.5s", "2m"). The number is decimal digits with an optional fraction. A
// bare number, an unknown unit, a sign or an exponent is an error. Digits
// beyond nanosecond precision are dropped.
func ParseDuration(s string) (time.Duration, error) {
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == '.') {
		end++
	}
	number, unit := s[:end], s[end:]
	if number == "" {
		return 0, errors.New("a duration is a number with a unit, like 500ms, 1.5s or 2m")
	}
	scale, ok := map[string]time.Duration{"ms": time.Millisecond, "s": time.Second, "m": time.Minute}[unit]
	if !ok {
		if unit == "" {
			return 0, errors.New("a duration needs a unit: ms, s or m (for example " + number + "s)")
		}
		return 0, errors.New("unknown duration unit " + strconv.Quote(unit) + ": use ms, s or m")
	}
	whole, frac, hasFrac := strings.Cut(number, ".")
	if whole == "" || hasFrac && (frac == "" || strings.Contains(frac, ".")) {
		return 0, errors.New("malformed number " + strconv.Quote(number))
	}
	n, err := strconv.ParseUint(whole, 10, 63)
	if err != nil || n > uint64(math.MaxInt64/scale) {
		return 0, errors.New("duration " + strconv.Quote(s) + " is too large")
	}
	d := time.Duration(n) * scale
	part := scale
	for i := 0; i < len(frac) && part > 1; i++ {
		part /= 10
		d += time.Duration(frac[i]-'0') * part
	}
	if d < 0 {
		return 0, errors.New("duration " + strconv.Quote(s) + " is too large")
	}
	return d, nil
}

// formatDuration is the canonical text of d: whole minutes as "Nm", whole
// seconds as "Ns", whole milliseconds as "Nms", otherwise decimal seconds.
// ParseDuration(formatDuration(d)) == d for every d >= 0.
func formatDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "0s"
	case d%time.Minute == 0:
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	case d%time.Second == 0:
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	case d%time.Millisecond == 0:
		return strconv.FormatInt(int64(d/time.Millisecond), 10) + "ms"
	}
	frac := strings.TrimRight(strconv.FormatInt(int64(d%time.Second)+int64(time.Second), 10)[1:], "0")
	return strconv.FormatInt(int64(d/time.Second), 10) + "." + frac + "s"
}
