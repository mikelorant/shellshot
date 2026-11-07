package run

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/script"
)

// defaultSettings are the behaviour settings before any script line.
func defaultSettings() Settings {
	return Settings{
		Typing:  DefaultTyping,
		Timeout: DefaultTimeout,
		Settle:  DefaultSettle,
	}
}

// isBehaviour reports whether name is a behaviour setting.
func isBehaviour(name string) bool {
	switch name {
	case "typing", "jitter", "timeout", "settle", "linger":
		return true
	}
	return false
}

// isHint reports whether name is an export hint setting.
func isHint(name string) bool {
	return name == "speed" || name == "idle" || name == "hold"
}

// apply sets the behaviour setting st in s. The value was checked by the
// validator, so an error here means the AST was not validated.
func (s *Settings) apply(st script.Setting) error {
	var err error
	switch st.Name {
	case "typing":
		s.Typing, err = script.ParseDuration(st.Value)
	case "timeout":
		s.Timeout, err = script.ParseDuration(st.Value)
	case "linger":
		s.Linger, err = script.ParseDuration(st.Value)
	case "settle":
		if st.Value == "off" {
			s.SettleOff = true
			break
		}
		s.Settle, err = script.ParseDuration(st.Value)
		s.SettleOff = false
	case "jitter":
		var pct float64
		pct, err = strconv.ParseFloat(strings.TrimSuffix(st.Value, "%"), 64)
		if err == nil && (pct < 0 || pct > 100 || math.IsNaN(pct) || !strings.HasSuffix(st.Value, "%")) {
			err = fmt.Errorf("jitter must be a percent from 0%% to 100%%")
		}
		if err == nil {
			s.Jitter = pct
		}
	default:
		err = fmt.Errorf("not a behaviour setting")
	}
	if err != nil {
		return fmt.Errorf("%s %q: %w", st.Name, st.Value, err)
	}
	return nil
}

// quiet is the quiet time that "wait idle" uses when it has no duration of
// its own, and the final drain uses: settle, or DefaultSettle if settle is off.
func (s Settings) quiet() time.Duration {
	if s.SettleOff {
		return DefaultSettle
	}
	return s.Settle
}

// applyHint sets the hint setting st in h.
func applyHint(h *event.SpanHints, st script.Setting) error {
	var err error
	switch st.Name {
	case "speed":
		var f float64
		f, err = strconv.ParseFloat(strings.TrimSuffix(st.Value, "x"), 64)
		milli := math.Round(f * 1000)
		if err == nil && (!strings.HasSuffix(st.Value, "x") || !(milli >= 1) || milli > event.MaxSpanSpeedMilli) {
			err = fmt.Errorf("speed must be above 0x and at most %dx", event.MaxSpanSpeedMilli/1000)
		}
		if err == nil {
			h.Speed = uint64(milli)
		}
	case "idle":
		h.Idle, err = script.ParseDuration(st.Value)
	case "hold":
		h.Hold, err = script.ParseDuration(st.Value)
	}
	if err != nil {
		return fmt.Errorf("%s %q: %w", st.Name, st.Value, err)
	}
	return nil
}

// applyProcess sets the process setting st in p.
func applyProcess(p *Process, st script.Setting) error {
	var err error
	switch st.Name {
	case "size":
		cols, rows, ok := strings.Cut(st.Value, "x")
		var c, r int
		if c, err = strconv.Atoi(cols); err == nil && ok {
			r, err = strconv.Atoi(rows)
		} else if !ok {
			err = fmt.Errorf("size is COLSxROWS")
		}
		if err == nil && (c < 1 || r < 1) {
			err = fmt.Errorf("size must be at least 1x1")
		}
		if err == nil {
			p.Cols, p.Rows = c, r
		}
	case "shell":
		if len(st.Argv) == 0 {
			err = fmt.Errorf("shell has no program")
			break
		}
		p.Shell = append([]string(nil), st.Argv...)
	case "env":
		if !strings.Contains(st.Value, "=") {
			err = fmt.Errorf("env is KEY=VALUE")
			break
		}
		p.Env = append(p.Env, st.Value)
	case "cwd":
		p.Cwd = st.Value
	case "output":
		p.Output = st.Value
	case "seed":
		var n uint64
		if n, err = strconv.ParseUint(st.Value, 10, 63); err == nil {
			p.Seed = int64(n)
		}
	default:
		err = fmt.Errorf("not a process setting")
	}
	if err != nil {
		return fmt.Errorf("%s %q: %w", st.Name, st.Value, err)
	}
	return nil
}
