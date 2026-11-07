package session

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/snapshot"
	"github.com/michaellorant/shellshot/internal/vt"
)

const testTimeout = time.Second

// ignoredByte is a byte the fake terminal drops, like a real terminal does
// with NUL. Writing it changes nothing on the screen.
const ignoredByte = "\x00"

// fakeTerm records writes and snapshots them as a single line of text.
type fakeTerm struct {
	mu        sync.Mutex
	text      strings.Builder
	cols      int
	rows      int
	closed    atomic.Bool
	writeErr  error
	written   chan struct{} // receives one value per Write
	snapErr   error
	snapshots atomic.Int32
	onWrite   func(p []byte) // called on each successful Write, if set
}

func (f *fakeTerm) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.mu.Lock()
	f.text.WriteString(strings.ReplaceAll(string(p), ignoredByte, ""))
	f.mu.Unlock()
	if f.onWrite != nil {
		f.onWrite(p)
	}
	f.written <- struct{}{}
	return len(p), nil
}

func (f *fakeTerm) Resize(cols, rows int) error { return nil }

func (f *fakeTerm) Snapshot() (snapshot.Snapshot, error) {
	f.snapshots.Add(1)
	if f.snapErr != nil {
		return snapshot.Snapshot{}, f.snapErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return snapshot.Snapshot{Cols: f.cols, Rows: f.rows, Lines: []string{f.text.String()}}, nil
}

func (f *fakeTerm) Close() error {
	f.closed.Store(true)
	return nil
}

// fakeProc is a Process fed through a channel.
type fakeProc struct {
	data     chan []byte // closing it means EOF
	closedCh chan struct{}
	once     sync.Once
	readers  atomic.Int32 // Read calls currently in flight
	exit     int
	waitErr  error
	readErr  error
	resizeTo [2]int
	resizeOK error

	wmu      sync.Mutex
	writes   [][]byte
	writeErr error
	onWrite  func(p []byte) // called on each successful Write, if set
}

func newFakeProc() *fakeProc {
	return &fakeProc{data: make(chan []byte), closedCh: make(chan struct{})}
}

func (p *fakeProc) Read(b []byte) (int, error) {
	p.readers.Add(1)
	defer p.readers.Add(-1)
	select {
	case d, ok := <-p.data:
		return p.deliver(b, d, ok)
	case <-p.closedCh:
		// Like a pty after the child exits, output that is already available
		// is delivered (or EOF reported) before the close is.
		select {
		case d, ok := <-p.data:
			return p.deliver(b, d, ok)
		default:
			return 0, io.ErrClosedPipe
		}
	}
}

// deliver completes a Read with a received chunk, or with the end of output
// when ok is false.
func (p *fakeProc) deliver(b, d []byte, ok bool) (int, error) {
	if !ok {
		if p.readErr != nil {
			return 0, p.readErr
		}
		return 0, io.EOF
	}
	return copy(b, d), nil
}

// Write records what a driver sends to the process.
func (p *fakeProc) Write(b []byte) (int, error) {
	if p.writeErr != nil {
		return 0, p.writeErr
	}
	if p.isClosed() {
		return 0, io.ErrClosedPipe
	}
	p.wmu.Lock()
	p.writes = append(p.writes, append([]byte(nil), b...))
	fn := p.onWrite
	p.wmu.Unlock()
	if fn != nil {
		fn(b)
	}
	return len(b), nil
}

func (p *fakeProc) Resize(cols, rows int) error {
	p.resizeTo = [2]int{cols, rows}
	return p.resizeOK
}

// Wait returns once the process is closed or its output has ended.
func (p *fakeProc) Wait() (int, error) {
	<-p.closedCh
	return p.exit, p.waitErr
}

func (p *fakeProc) Close() error {
	p.once.Do(func() { close(p.closedCh) })
	return nil
}

func (p *fakeProc) isClosed() bool {
	select {
	case <-p.closedCh:
		return true
	default:
		return false
	}
}

// exitWith lets Wait return, as if the child had exited.
func (p *fakeProc) exitWith(code int) { p.exit = code; p.Close() }

// fakeClock is a manually advanced clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// harness wires fakes around Run.
type harness struct {
	t       *testing.T
	proc    *fakeProc
	term    *fakeTerm
	clock   *fakeClock
	ticks   chan time.Time
	frames  chan snapshot.Frame
	result  chan runResult
	cancel  context.CancelFunc
	sinkErr error // returned by the sink when frameFail reaches zero
	stopped atomic.Bool
	period  atomic.Int64 // duration the ticker was created with
	fps     int
	factory vt.Factory
	cols    int
	rows    int
	events  EventSink // Config.Events; nil by default
	onStart func(time.Time)
	onTick  func(time.Time) error
	driver  Driver        // Config.Driver; nil by default
	grace   time.Duration // Config.ShutdownGrace

	tickers atomic.Int32 // Ticker calls: the first is the sampler, later ones are timers
	tmu     sync.Mutex
	timers  []*fakeTimer // timers made through Ticker after the sampler
}

// fakeTimer is a timer made through Config.Ticker; tests fire it by hand.
type fakeTimer struct {
	d       time.Duration
	c       chan time.Time
	stopped atomic.Bool
}

type runResult struct {
	code int
	err  error
}

// startRun launches Run. failOnFrame, if > 0, makes the sink return failErr
// on that (1-based) frame.
func startRun(t *testing.T, failOnFrame int, failErr error, mutate func(*harness)) *harness {
	t.Helper()
	h := &harness{
		t:      t,
		proc:   newFakeProc(),
		term:   &fakeTerm{written: make(chan struct{}, 100)},
		clock:  &fakeClock{t: time.Unix(1000, 0)},
		ticks:  make(chan time.Time),
		frames: make(chan snapshot.Frame, 100),
		result: make(chan runResult, 1),
	}
	if mutate != nil {
		mutate(h)
	}
	factory := h.factory
	if factory == nil {
		factory = func(cols, rows int) (vt.Terminal, error) {
			h.term.cols, h.term.rows = cols, rows
			return h.term, nil
		}
	}
	var n int
	sink := SinkFunc(func(f snapshot.Frame) error {
		n++
		h.frames <- f
		if failOnFrame > 0 && n == failOnFrame {
			return failErr
		}
		return nil
	})
	cfg := Config{
		FPS:     h.fps,
		Cols:    h.cols,
		Rows:    h.rows,
		Events:  h.events,
		OnStart: h.onStart,
		OnTick:  h.onTick,
		Driver:  h.driver,

		ShutdownGrace: h.grace,
		Clock:         h.clock.now,
		Ticker: func(d time.Duration) (<-chan time.Time, func()) {
			if h.tickers.Add(1) > 1 {
				t := &fakeTimer{d: d, c: make(chan time.Time, 1)}
				h.tmu.Lock()
				h.timers = append(h.timers, t)
				h.tmu.Unlock()
				return t.c, func() { t.stopped.Store(true) }
			}
			h.period.Store(int64(d))
			return h.ticks, func() { h.stopped.Store(true) }
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	t.Cleanup(cancel)
	go func() {
		code, err := Run(ctx, h.proc, factory, cfg, sink)
		h.result <- runResult{code, err}
	}()
	return h
}

func (h *harness) nextFrame() snapshot.Frame {
	h.t.Helper()
	select {
	case f := <-h.frames:
		return f
	case <-time.After(testTimeout):
		h.t.Fatal("timed out waiting for frame")
		return snapshot.Frame{}
	}
}

func (h *harness) noFrame() {
	h.t.Helper()
	select {
	case f := <-h.frames:
		h.t.Fatalf("unexpected frame %+v", f)
	default:
	}
}

// output sends data to the process and waits until the terminal has it.
func (h *harness) output(s string) {
	h.t.Helper()
	h.send(h.proc.data, []byte(s))
	select {
	case <-h.term.written:
	case <-time.After(testTimeout):
		h.t.Fatal("timed out waiting for terminal write")
	}
}

func (h *harness) send(ch chan []byte, b []byte) {
	h.t.Helper()
	select {
	case ch <- b:
	case <-time.After(testTimeout):
		h.t.Fatal("timed out sending process output")
	}
}

// tick delivers one tick and returns once the loop has taken it.
func (h *harness) tick() {
	h.t.Helper()
	select {
	case h.ticks <- time.Time{}:
	case <-time.After(testTimeout):
		h.t.Fatal("timed out sending tick")
	}
}

// settle returns once any tick delivered earlier has been fully handled: the
// loop only takes the next tick after finishing the previous one.
func (h *harness) settle() { h.tick() }

func (h *harness) wait() runResult {
	h.t.Helper()
	select {
	case r := <-h.result:
		return r
	case <-time.After(testTimeout):
		h.t.Fatal("timed out waiting for Run to return")
		return runResult{}
	}
}

// assertClean checks that Run released everything it owns.
func (h *harness) assertClean() {
	h.t.Helper()
	if !h.term.closed.Load() {
		h.t.Error("terminal not closed")
	}
	if !h.proc.isClosed() {
		h.t.Error("process not closed")
	}
	if n := h.proc.readers.Load(); n != 0 {
		h.t.Errorf("%d reader(s) still running after Run returned", n)
	}
	if !h.stopped.Load() {
		h.t.Error("ticker not stopped")
	}
}

func TestRunEmitsInitialFrameAtZeroBeforeOutput(t *testing.T) {
	h := startRun(t, 0, nil, func(h *harness) { h.cols, h.rows = 40, 10 })
	f := h.nextFrame()
	if f.At != 0 {
		t.Errorf("At = %v, want 0", f.At)
	}
	if f.Snapshot.Cols != 40 || f.Snapshot.Rows != 10 || f.Snapshot.Lines[0] != "" {
		t.Errorf("initial snapshot = %+v", f.Snapshot)
	}
	if h.proc.resizeTo != [2]int{40, 10} {
		t.Errorf("process resized to %v, want [40 10]", h.proc.resizeTo)
	}
	h.cancel()
	h.wait()
}

func TestRunDefaultsSize(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	f := h.nextFrame()
	if f.Snapshot.Cols != DefaultCols || f.Snapshot.Rows != DefaultRows {
		t.Errorf("size = %dx%d, want %dx%d", f.Snapshot.Cols, f.Snapshot.Rows, DefaultCols, DefaultRows)
	}
	h.cancel()
	h.wait()
}

func TestRunEmitsOnlyChangedFrames(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	h.nextFrame() // initial

	h.tick()
	h.settle()
	h.noFrame() // nothing written, nothing emitted

	h.clock.advance(300 * time.Millisecond)
	h.output("a")
	h.tick()
	f := h.nextFrame()
	if f.At != 300*time.Millisecond || f.Snapshot.Lines[0] != "a" {
		t.Errorf("frame = %+v, want At=300ms text a", f)
	}

	h.tick()
	h.settle()
	h.noFrame() // unchanged since last frame

	h.clock.advance(200 * time.Millisecond)
	h.output("b")
	h.tick()
	f = h.nextFrame()
	if f.At != 500*time.Millisecond || f.Snapshot.Lines[0] != "ab" {
		t.Errorf("frame = %+v, want At=500ms text ab", f)
	}

	h.cancel()
	h.wait()
}

func TestRunDoesNotEmitWhenWriteLeavesScreenUnchanged(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	h.nextFrame()
	h.output(ignoredByte) // a write that does not alter the fake's screen
	h.tick()
	h.settle()
	h.noFrame()
	if n := h.term.snapshots.Load(); n != 2 { // initial frame + the sampled tick
		t.Errorf("snapshots = %d, want 2 (tick must have sampled the screen)", n)
	}
	h.cancel()
	h.wait()
}

func TestRunFinalFrameAfterDrainingOutputAndExitCode(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	h.nextFrame()

	h.clock.advance(time.Second)
	h.output("done")
	h.proc.exit = 7
	close(h.proc.data)
	h.proc.Close() // the child has exited

	f := h.nextFrame()
	if f.At != time.Second || f.Snapshot.Lines[0] != "done" {
		t.Errorf("final frame = %+v", f)
	}
	r := h.wait()
	if r.err != nil || r.code != 7 {
		t.Errorf("Run = (%d, %v), want (7, nil)", r.code, r.err)
	}
	h.noFrame()
	h.assertClean()
}

func TestRunFinalFrameIsEmittedEvenWhenUnchanged(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	h.nextFrame()
	h.clock.advance(time.Second)
	h.output("x")
	h.tick()
	h.nextFrame() // "x" emitted on tick

	h.clock.advance(time.Second)
	close(h.proc.data)
	h.proc.exitWith(0)

	f := h.nextFrame()
	if f.At != 2*time.Second || f.Snapshot.Lines[0] != "x" {
		t.Errorf("final frame = %+v, want At=2s text x", f)
	}
	if r := h.wait(); r.err != nil || r.code != 0 {
		t.Errorf("Run = (%d, %v)", r.code, r.err)
	}
	h.noFrame()
}

func TestRunFinalFrameWithoutAnyOutput(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	h.nextFrame()
	close(h.proc.data)
	h.proc.exitWith(3)
	h.nextFrame() // final frame, identical to the initial one
	if r := h.wait(); r.err != nil || r.code != 3 {
		t.Errorf("Run = (%d, %v), want (3, nil)", r.code, r.err)
	}
	h.assertClean()
}

func TestRunPropagatesWaitError(t *testing.T) {
	wantErr := errors.New("wait failed")
	h := startRun(t, 0, nil, func(h *harness) { h.proc.waitErr = wantErr })
	h.nextFrame()
	close(h.proc.data)
	h.proc.exitWith(0)
	r := h.wait()
	if !errors.Is(r.err, wantErr) || r.code != -1 {
		t.Errorf("Run = (%d, %v), want (-1, wait error)", r.code, r.err)
	}
}

func TestRunSinkErrorAborts(t *testing.T) {
	sinkErr := errors.New("disk full")
	h := startRun(t, 2, sinkErr, nil)
	h.nextFrame()
	h.output("a")
	h.tick() // second frame -> sink fails
	h.nextFrame()
	r := h.wait()
	if !errors.Is(r.err, sinkErr) || r.code != -1 {
		t.Errorf("Run = (%d, %v), want (-1, sink error)", r.code, r.err)
	}
	h.assertClean()
}

func TestRunInitialSinkErrorAborts(t *testing.T) {
	sinkErr := errors.New("nope")
	h := startRun(t, 1, sinkErr, nil)
	r := h.wait()
	if !errors.Is(r.err, sinkErr) {
		t.Errorf("err = %v, want sink error", r.err)
	}
	h.assertClean()
}

func TestRunFinalFrameSinkErrorAborts(t *testing.T) {
	sinkErr := errors.New("final failed")
	h := startRun(t, 2, sinkErr, nil)
	h.nextFrame()
	close(h.proc.data)
	h.proc.exitWith(0)
	r := h.wait()
	if !errors.Is(r.err, sinkErr) {
		t.Errorf("err = %v, want sink error", r.err)
	}
	h.assertClean()
}

func TestRunContextCancel(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	h.nextFrame()
	h.output("partial")
	h.cancel()
	r := h.wait()
	if !errors.Is(r.err, context.Canceled) || r.code != -1 {
		t.Errorf("Run = (%d, %v), want (-1, context.Canceled)", r.code, r.err)
	}
	h.assertClean()
}

func TestRunContextCancelDuringWait(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	h.nextFrame()
	close(h.proc.data) // output ends but the process has not exited yet
	h.nextFrame()      // final frame; Run is now blocked in Wait
	h.cancel()
	r := h.wait()
	if !errors.Is(r.err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", r.err)
	}
	h.assertClean()
}

func TestRunAlreadyCancelledContext(t *testing.T) {
	proc := newFakeProc()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	created := false
	code, err := Run(ctx, proc, func(int, int) (vt.Terminal, error) {
		created = true
		return nil, errors.New("unreachable")
	}, Config{}, SinkFunc(func(snapshot.Frame) error { return nil }))
	if !errors.Is(err, context.Canceled) || code != -1 {
		t.Errorf("Run = (%d, %v)", code, err)
	}
	if created {
		t.Error("terminal created despite cancelled context")
	}
	if !proc.isClosed() {
		t.Error("process not closed")
	}
}

func TestRunReadErrorAborts(t *testing.T) {
	readErr := errors.New("boom")
	h := startRun(t, 0, nil, func(h *harness) { h.proc.readErr = readErr })
	h.nextFrame()
	close(h.proc.data)
	r := h.wait()
	if !errors.Is(r.err, readErr) || r.code != -1 {
		t.Errorf("Run = (%d, %v), want (-1, read error)", r.code, r.err)
	}
	h.assertClean()
}

func TestRunTerminalWriteErrorAborts(t *testing.T) {
	writeErr := errors.New("bad escape")
	h := startRun(t, 0, nil, func(h *harness) { h.term.writeErr = writeErr })
	h.nextFrame()
	h.send(h.proc.data, []byte("x"))
	r := h.wait()
	if !errors.Is(r.err, writeErr) {
		t.Errorf("err = %v, want write error", r.err)
	}
	h.assertClean()
}

func TestRunSnapshotErrorAborts(t *testing.T) {
	snapErr := errors.New("snapshot failed")
	h := startRun(t, 0, nil, func(h *harness) { h.term.snapErr = snapErr })
	r := h.wait()
	if !errors.Is(r.err, snapErr) {
		t.Errorf("err = %v, want snapshot error", r.err)
	}
	h.assertClean()
}

func TestRunTerminalFactoryError(t *testing.T) {
	factoryErr := errors.New("no terminal")
	proc := newFakeProc()
	code, err := Run(context.Background(), proc, func(int, int) (vt.Terminal, error) { return nil, factoryErr },
		Config{}, SinkFunc(func(snapshot.Frame) error { return nil }))
	if !errors.Is(err, factoryErr) || code != -1 {
		t.Errorf("Run = (%d, %v)", code, err)
	}
	if !proc.isClosed() {
		t.Error("process not closed")
	}
}

func TestRunResizeErrorAborts(t *testing.T) {
	resizeErr := errors.New("resize failed")
	h := startRun(t, 0, nil, func(h *harness) { h.proc.resizeOK = resizeErr })
	r := h.wait()
	if !errors.Is(r.err, resizeErr) {
		t.Errorf("err = %v, want resize error", r.err)
	}
	if !h.term.closed.Load() || !h.proc.isClosed() {
		t.Error("resources not closed")
	}
}

func TestRunDoesNotLeakGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 5; i++ {
		h := startRun(t, 0, nil, nil)
		h.nextFrame()
		h.output("x")
		h.cancel()
		h.wait()
		h.assertClean()
	}
	deadline := time.Now().Add(testTimeout)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutines: %d before, %d after", before, after)
	}
}

func TestSinkFuncCallsFunction(t *testing.T) {
	want := errors.New("x")
	var got snapshot.Frame
	var s Sink = SinkFunc(func(f snapshot.Frame) error { got = f; return want })
	f := snapshot.Frame{At: time.Second}
	if err := s.Frame(f); err != want || got.At != time.Second {
		t.Errorf("SinkFunc: err=%v got=%+v", err, got)
	}
}

func TestConfigWithDefaults(t *testing.T) {
	c := Config{}.withDefaults()
	if c.FPS != 60 || c.Cols != 80 || c.Rows != 24 || c.Clock == nil || c.Ticker == nil {
		t.Errorf("defaults = %+v", c)
	}
	c = Config{FPS: -3}.withDefaults()
	if c.FPS != DefaultFPS {
		t.Errorf("negative FPS = %d, want %d", c.FPS, DefaultFPS)
	}
	c = Config{FPS: 30, Cols: 10, Rows: 5}.withDefaults()
	if c.FPS != 30 || c.Cols != 10 || c.Rows != 5 {
		t.Errorf("explicit values overridden: %+v", c)
	}
}

func TestConfigPeriod(t *testing.T) {
	for _, tc := range []struct {
		fps  int
		want time.Duration
	}{
		{0, 16667 * time.Microsecond},
		{-1, 16667 * time.Microsecond},
		{60, 16667 * time.Microsecond},
		{1, time.Second},
		{10, 100 * time.Millisecond},
		{30, 33333 * time.Microsecond},
		{1000, time.Millisecond},
	} {
		if got := (Config{FPS: tc.fps}).Period(); got != tc.want {
			t.Errorf("Config{FPS: %d}.Period() = %v, want %v", tc.fps, got, tc.want)
		}
	}
}

func TestRunTicksAtDefault60FPS(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	h.nextFrame()
	if got, want := time.Duration(h.period.Load()), 16667*time.Microsecond; got != want {
		t.Errorf("ticker period = %v, want %v", got, want)
	}
	h.cancel()
	h.wait()
}

func TestRunUsesConfiguredFPS(t *testing.T) {
	h := startRun(t, 0, nil, func(h *harness) { h.fps = 10 })
	h.nextFrame()
	if got, want := time.Duration(h.period.Load()), 100*time.Millisecond; got != want {
		t.Errorf("ticker period = %v, want %v", got, want)
	}
	h.cancel()
	h.wait()
}

// At 60fps an idle session ticks constantly; those ticks must not snapshot the
// terminal, and output that arrives between two ticks is captured by the next.
func TestRunIdleTicksTakeNoSnapshotsAt60FPS(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	period := Config{}.Period()
	h.nextFrame()   // initial frame: one snapshot
	for range 120 { // two seconds of idle ticks
		h.tick()
	}
	h.settle()
	h.noFrame()
	if n := h.term.snapshots.Load(); n != 1 {
		t.Fatalf("snapshots after idle ticks = %d, want 1 (the initial frame only)", n)
	}

	h.clock.advance(period / 2) // change within one tick period
	h.output("x")
	if n := h.term.snapshots.Load(); n != 1 {
		t.Errorf("snapshots before next tick = %d, want 1", n)
	}
	h.tick()
	f := h.nextFrame()
	if f.Snapshot.Lines[0] != "x" || f.At != period/2 {
		t.Errorf("frame = %+v, want text x at half a period", f)
	}
	h.tick()
	h.settle()
	h.noFrame()
	if n := h.term.snapshots.Load(); n != 2 {
		t.Errorf("snapshots = %d, want 2 (initial + one dirty tick)", n)
	}
	h.cancel()
	h.wait()
}

func TestRunRejectsFPSAboveMax(t *testing.T) {
	proc := newFakeProc()
	code, err := Run(context.Background(), proc, func(int, int) (vt.Terminal, error) {
		t.Error("terminal created despite invalid fps")
		return nil, errors.New("unreachable")
	}, Config{FPS: MaxFPS + 1}, SinkFunc(func(snapshot.Frame) error { return nil }))
	if !errors.Is(err, ErrFPS) || code != -1 {
		t.Errorf("Run = (%d, %v), want ErrFPS", code, err)
	}
	if !proc.isClosed() {
		t.Error("process not closed")
	}
}
