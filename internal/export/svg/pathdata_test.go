package svg

import (
	"math/big"
	"math/rand"
	"strings"
	"testing"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// TestPathDataIsArchitectureIndependent checks the coordinates of pathData
// against exact big.Float arithmetic, which does not depend on whether the
// CPU fuses x*y+z (arm64 does, amd64 does not). It also requires that
// unfused float64 arithmetic prints differently from the exact result
// somewhere in the sample, so the sample does distinguish the two. A plain
// x*y+z in pathData equals the exact result on arm64 and so is caught only
// on amd64.
func TestPathDataIsArchitectureIndependent(t *testing.T) {
	const (
		scale  = 15.0 / 1000 // font size over units per em
		ox, oy = 0.4, 11.6
	)
	rng := rand.New(rand.NewSource(1))
	var segs sfnt.Segments
	var want []string
	unfusedDiffers := 0
	for range 100000 {
		// Whole font units, as in real outlines: at a 0.015 scale an odd unit lands on a rounding tie.
		p := fixed.P(rng.Intn(2000)-500, rng.Intn(2000)-500)
		op := sfnt.SegmentOpLineTo
		if len(segs) == 0 {
			op = sfnt.SegmentOpMoveTo
		}
		segs = append(segs, sfnt.Segment{Op: op, Args: [3]fixed.Point26_6{p}})
		x, y := float64(p.X)/64, float64(p.Y)/64
		want = append(want, num(exactSum(x, scale, ox)), num(exactSum(y, scale, oy)))
		// The explicit conversion forbids fusing, on every architecture.
		if num(ox+float64(x*scale)) != want[len(want)-2] {
			unfusedDiffers++
		}
	}
	if unfusedDiffers == 0 {
		t.Fatal("no sample point where unfused arithmetic differs from exact; the test cannot detect fusion")
	}

	d := pathData(segs, scale, ox, oy)
	got := strings.Fields(strings.NewReplacer("M", " ", "L", " ", "z", " ").Replace(d))
	if len(got) != len(want) {
		t.Fatalf("got %d coordinates, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("coordinate %d = %s, exact arithmetic gives %s", i, got[i], want[i])
		}
	}
}

// exactSum returns origin + v*scale rounded once to float64, which is what a
// fused multiply-add computes.
func exactSum(v, scale, origin float64) float64 {
	x := new(big.Float).SetPrec(512).SetFloat64(v)
	x.Mul(x, new(big.Float).SetPrec(512).SetFloat64(scale))
	x.Add(x, new(big.Float).SetPrec(512).SetFloat64(origin))
	f, _ := x.Float64()
	return f
}
