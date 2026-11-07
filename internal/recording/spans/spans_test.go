package spans

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
)

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func begin(id uint64, typ event.SpanType, parent uint64, name string, h event.SpanHints) event.Span {
	return event.Span{Op: event.SpanBegin, ID: id, Type: typ, Parent: parent, Name: name, Hints: h}
}

func end(id uint64) event.Span { return event.Span{Op: event.SpanEnd, ID: id} }

type step struct {
	at   int // ms
	span *event.Span
	out  string
}

func write(t *testing.T, steps []step, o bin.WriterOptions) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := bin.NewWriter(&buf, head.Head{Cols: 80, Rows: 24}, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if s.span != nil {
			err = w.Span(ms(s.at), *s.span)
		} else {
			err = w.Output(ms(s.at), []byte(s.out))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func scan(t *testing.T, file []byte) ([]Span, error) {
	t.Helper()
	r, err := bin.NewReader(bytes.NewReader(file), bin.ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return Scan(r)
}

func sp(s event.Span) *event.Span { return &s }

func TestScanNestingAndHints(t *testing.T) {
	hints := event.SpanHints{Speed: 8000, Idle: 2 * time.Second, Hold: time.Second}
	file := write(t, []step{
		{at: 0, out: "$ "},
		{at: 10, span: sp(begin(1, event.SpanScene, 0, "install", hints))},
		{at: 20, out: "x"},
		{at: 30, span: sp(begin(2, event.SpanHidden, 1, "", event.SpanHints{}))},
		{at: 40, out: "y"},
		{at: 50, span: sp(end(2))},
		{at: 60, span: sp(end(1))},
		{at: 70, span: sp(begin(3, event.SpanHidden, 0, "", event.SpanHints{}))},
		{at: 80, span: sp(end(3))},
		{at: 90, out: "bye"},
	}, bin.WriterOptions{CoalesceTicks: 50})
	got, err := scan(t, file)
	if err != nil {
		t.Fatal(err)
	}
	want := []Span{
		{ID: 1, Type: event.SpanScene, Name: "install", Hints: hints, Start: ms(10), End: ms(60)},
		{ID: 2, Type: event.SpanHidden, Parent: 1, Depth: 1, Start: ms(30), End: ms(50)},
		{ID: 3, Type: event.SpanHidden, Start: ms(70), End: ms(80)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans = %+v\nwant    %+v", got, want)
	}
	if got[0].Duration() != ms(50) {
		t.Errorf("Duration = %v", got[0].Duration())
	}
}

func TestScanOpenSpanAtCleanEOF(t *testing.T) {
	file := write(t, []step{
		{at: 0, span: sp(begin(1, event.SpanScene, 0, "a", event.SpanHints{}))},
		{at: 10, span: sp(begin(2, event.SpanHidden, 1, "", event.SpanHints{}))},
		{at: 25, out: "z"},
	}, bin.WriterOptions{})
	got, err := scan(t, file)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Open || !got[1].Open || got[0].End != ms(25) || got[1].End != ms(25) {
		t.Fatalf("spans = %+v", got)
	}
}

func TestScanTornTailWithOpenSpan(t *testing.T) {
	var buf bytes.Buffer
	w, err := bin.NewWriter(&buf, head.Head{Cols: 80, Rows: 24}, bin.WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		w.Span(0, begin(1, event.SpanScene, 0, "demo", event.SpanHints{})),
		w.Output(ms(5), []byte("hello")),
		w.Flush(),
		w.Output(ms(9), []byte("lost")),
		w.Span(ms(12), end(1)),
		w.Close(),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	torn := buf.Bytes()[:buf.Len()-2]
	got, err := scan(t, torn)
	if !errors.Is(err, bin.ErrTruncated) {
		t.Fatalf("err = %v, want ErrTruncated", err)
	}
	want := []Span{{ID: 1, Type: event.SpanScene, Name: "demo", End: ms(5), Open: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans = %+v, want %+v", got, want)
	}
}

// fakeSource replays events built by hand, for payloads a Writer refuses to write.
type fakeSource struct {
	evs []bin.Event
	err error
}

func (f *fakeSource) Next() (bin.Event, error) {
	if len(f.evs) == 0 {
		if f.err != nil {
			return bin.Event{}, f.err
		}
		return bin.Event{}, io.EOF
	}
	e := f.evs[0]
	f.evs = f.evs[1:]
	return e, nil
}

func spanEv(at int, s event.Span) bin.Event {
	return bin.Event{At: ms(at), Kind: event.KindSpan, Data: event.AppendSpan(nil, s)}
}

func TestScanIgnorableEvents(t *testing.T) {
	src := &fakeSource{evs: []bin.Event{
		{At: 0, Kind: event.KindOut, Data: []byte("a")},
		spanEv(1, end(9)), // unknown id
		spanEv(2, begin(1, event.SpanScene, 0, "a", event.SpanHints{})),
		spanEv(3, begin(1, event.SpanScene, 0, "dup", event.SpanHints{})),    // duplicate id
		spanEv(4, begin(2, event.SpanScene, 0, "nested", event.SpanHints{})), // scene in scene
		spanEv(5, begin(3, event.SpanHidden, 7, "", event.SpanHints{})),      // parent is not the open scene
		spanEv(6, begin(4, event.SpanBeat, 1, "beat", event.SpanHints{})),    // reserved
		{At: ms(7), Kind: event.KindSpan, Data: []byte{2, 1, 5, 1, 0, 0, 0}}, // version 2
		{At: ms(8), Kind: event.KindSpan, Data: []byte{1, 1, 6, 9, 0, 0, 0}}, // type 9
		spanEv(9, end(2)), // the ignored nested scene was never opened
		spanEv(10, begin(5, event.SpanHidden, 1, "", event.SpanHints{})),
		spanEv(11, begin(6, event.SpanHidden, 1, "", event.SpanHints{})), // hidden in hidden
		spanEv(12, end(1)), // ends the scene and the hidden span inside it
		spanEv(13, end(5)), // already closed
		{At: ms(14), Kind: event.KindMark, Data: []byte("m")},
	}}
	got, err := Scan(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []Span{
		{ID: 1, Type: event.SpanScene, Name: "a", Start: ms(2), End: ms(12)},
		{ID: 5, Type: event.SpanHidden, Parent: 1, Depth: 1, Start: ms(10), End: ms(12)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans = %+v\nwant    %+v", got, want)
	}
}

func TestScanMalformedSpan(t *testing.T) {
	src := &fakeSource{evs: []bin.Event{
		spanEv(1, begin(1, event.SpanScene, 0, "a", event.SpanHints{})),
		{At: ms(2), Kind: event.KindSpan, Data: []byte{1, 1}},
	}}
	got, err := Scan(src)
	if !errors.Is(err, event.ErrMalformedSpan) {
		t.Fatalf("err = %v, want ErrMalformedSpan", err)
	}
	if len(got) != 1 || !got[0].Open || got[0].End != ms(2) {
		t.Fatalf("spans = %+v", got)
	}
}

func TestScanNoSpans(t *testing.T) {
	got, err := Scan(&fakeSource{evs: []bin.Event{{Kind: event.KindOut, Data: []byte("x")}}})
	if err != nil || len(got) != 0 {
		t.Fatalf("spans = %+v, err = %v", got, err)
	}
}
