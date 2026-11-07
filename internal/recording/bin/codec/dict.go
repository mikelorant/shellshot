package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"slices"

	"github.com/klauspost/compress/zstd"
)

// dictGram is the shingle length used to score how much a sample shares with
// the others.
const dictGram = 8

// BuildDict returns a naive raw-content dictionary of at most size bytes made
// of whole or truncated samples. It is a prefix/history dictionary, not a
// trained one: the bytes are used verbatim as pre-history by the compressor.
//
// Samples are chosen greedily by how many 8-byte sequences they share with
// other samples that the dictionary does not already cover, per byte. The
// chosen samples are concatenated with the most useful last, because
// back-references are cheapest close to the data being compressed. The result
// is deterministic.
//
// The output is usable as the dict argument of every codec here: DEFLATE only
// looks at the last 32 KiB, and ZstdCodec treats bytes without the zstd
// dictionary magic as raw content. Use BuildZstdDict for a dictionary with
// entropy tables.
//
// Cost is quadratic in the number of samples; intended for tens to hundreds of
// samples of a few KiB. It returns nil if size <= 0 or there are no samples.
func BuildDict(samples [][]byte, size int) []byte {
	if size <= 0 {
		return nil
	}
	grams := make([][]uint64, len(samples)) // distinct grams per sample
	docFreq := make(map[uint64]int)
	for i, s := range samples {
		seen := make(map[uint64]struct{})
		for j := 0; j+dictGram <= len(s); j++ {
			g := binary.LittleEndian.Uint64(s[j:])
			if _, dup := seen[g]; dup {
				continue
			}
			seen[g] = struct{}{}
			grams[i] = append(grams[i], g)
			docFreq[g]++
		}
	}

	covered := make(map[uint64]struct{})
	remaining := make([]int, 0, len(samples))
	for i, s := range samples {
		if len(s) > 0 {
			remaining = append(remaining, i)
		}
	}
	var chosen []int // best first
	used := 0
	for len(remaining) > 0 && used < size {
		best, bestPos := -1, -1
		var bestDensity float64
		for pos, i := range remaining {
			score := 0
			for _, g := range grams[i] {
				if _, ok := covered[g]; !ok {
					score += docFreq[g] - 1
				}
			}
			density := float64(score) / float64(len(samples[i]))
			if best < 0 || density > bestDensity {
				best, bestPos, bestDensity = i, pos, density
			}
		}
		remaining = slices.Delete(remaining, bestPos, bestPos+1)
		for _, g := range grams[best] {
			covered[g] = struct{}{}
		}
		chosen = append(chosen, best)
		used += len(samples[best])
	}

	if len(chosen) == 0 {
		return nil
	}

	// Fill from the end so the best sample is last; truncate the overflow
	// from the front of the least useful sample that does not fit.
	out := make([]byte, 0, min(used, size))
	for k := len(chosen) - 1; k >= 0; k-- {
		out = append(out, samples[chosen[k]]...)
	}
	if len(out) > size {
		out = out[len(out)-size:]
	}
	return out
}

// BuildZstdDict returns a dictionary in the zstd dictionary format built from
// samples with zstd.BuildDict: BuildDict's content (at most size bytes, at
// least 8) is used as history and the samples train the entropy and offset
// tables. The dictionary id is derived from the content.
//
// The result is for ZstdCodec only; it is larger than size by the table
// header. The zstd package builds dictionaries for a given encoder level;
// SpeedBestCompression is used.
func BuildZstdDict(samples [][]byte, size int) ([]byte, error) {
	hist := BuildDict(samples, size)
	if len(hist) < 8 {
		return nil, errors.New("codec: not enough sample data for a zstd dictionary")
	}
	d, err := zstd.BuildDict(zstd.BuildDictOptions{
		ID:       crc32.ChecksumIEEE(hist) | 1<<31,
		Contents: samples,
		History:  hist,
		Offsets:  [3]int{1, 4, 8},
		Level:    zstd.SpeedBestCompression,
	})
	if err != nil {
		return nil, fmt.Errorf("codec: building zstd dictionary: %w", err)
	}
	return d, nil
}
