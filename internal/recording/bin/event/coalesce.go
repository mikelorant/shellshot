package event

// DefaultMaxMerged is the default size cap, in bytes, of a merged KindOut
// event.
const DefaultMaxMerged = 16 << 10

// Coalescer merges runs of adjacent KindOut events into one event on the
// writer side, before blocks are encoded.
//
// An OUT event is merged into the pending one when its tick is within
// gapTicks of the pending event's tick (the FIRST tick of the merged run, so
// the window is anchored and a long steady stream is split into runs rather
// than chained indefinitely) and the merged data stays within maxBytes. The
// merged event keeps the first tick: timing of later constituents is
// quantised to the start of the window, by at most gapTicks. Events of any
// other kind are never merged and never reordered: they flush the pending
// OUT event first.
//
// A pending event is held until a later Add or Flush, so the writer must call
// Flush on its own flush policy (size, timer, close). A Coalescer is not safe
// for concurrent use.
type Coalescer struct {
	gapTicks uint64
	maxBytes int

	pending Event
	has     bool
}

// NewCoalescer returns a Coalescer that merges OUT events whose ticks are at
// most gapTicks after the first tick of the run. maxBytes caps the merged data
// size; values <= 0 use DefaultMaxMerged. A single event larger than maxBytes
// is passed through whole but is never merged with others.
func NewCoalescer(gapTicks uint64, maxBytes int) *Coalescer {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxMerged
	}
	return &Coalescer{gapTicks: gapTicks, maxBytes: maxBytes}
}

// Add offers e and returns the events that are now complete, in order. The
// result is empty while e is being held for possible merging. Add copies
// e.Data of OUT events it holds, so the caller may reuse its buffer; events
// passed through (non-OUT) are returned as given.
func (c *Coalescer) Add(e Event) (flush []Event) {
	if e.Kind != KindOut {
		flush = c.take(flush)
		return append(flush, e)
	}
	if c.has && e.Tick >= c.pending.Tick &&
		e.Tick-c.pending.Tick <= c.gapTicks &&
		len(c.pending.Data)+len(e.Data) <= c.maxBytes {
		c.pending.Data = append(c.pending.Data, e.Data...)
		return nil
	}
	flush = c.take(flush)
	c.pending = Event{Tick: e.Tick, Kind: KindOut, Data: append([]byte(nil), e.Data...)}
	c.has = true
	return flush
}

// Flush returns the pending event, if any, and clears the Coalescer.
func (c *Coalescer) Flush() []Event {
	return c.take(nil)
}

func (c *Coalescer) take(dst []Event) []Event {
	if !c.has {
		return dst
	}
	e := c.pending
	c.pending = Event{}
	c.has = false
	if len(e.Data) == 0 {
		e.Data = nil
	}
	return append(dst, e)
}
