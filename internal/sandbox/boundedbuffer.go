package sandbox

import (
	"fmt"
	"unicode/utf8"
)

// boundedBuffer is an io.Writer that keeps only the first limit bytes written
// to it and counts the rest, so a chatty command cannot grow worker memory.
// It never reports a short write: the producer must keep draining.
type boundedBuffer struct {
	limit   int
	buf     []byte
	dropped int64
}

func newBoundedBuffer(limit int) *boundedBuffer {
	if limit < 0 {
		limit = 0
	}
	return &boundedBuffer{limit: limit}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	room := b.limit - len(b.buf)
	if room > len(p) {
		room = len(p)
	}
	if room > 0 {
		b.buf = append(b.buf, p[:room]...)
	}
	b.dropped += int64(len(p) - room)
	return len(p), nil
}

func (b *boundedBuffer) Dropped() int64 { return b.dropped }

func (b *boundedBuffer) Truncated() bool { return b.dropped > 0 }

// String returns the kept output, followed by a truncation notice when bytes
// were dropped.
func (b *boundedBuffer) String() string {
	if b.dropped == 0 {
		return string(b.buf)
	}
	kept := b.buf
	dropped := b.dropped
	// The cut may land inside a multi-byte rune; drop the partial tail.
	for i := 0; i < utf8.UTFMax-1 && len(kept) > 0; i++ {
		r, size := utf8.DecodeLastRune(kept)
		if r != utf8.RuneError || size != 1 {
			break
		}
		kept = kept[:len(kept)-1]
		dropped++
	}
	return string(kept) + fmt.Sprintf("\n[output truncated: %d bytes dropped, limit %d bytes]", dropped, b.limit)
}
