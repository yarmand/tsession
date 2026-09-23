package codeserver

import "sync"

// logBufferCapacity bounds the amount of `code serve-web` output retained
// per instance, so the "starting" status endpoint can show a recent log
// tail without unbounded memory growth for a long-running instance.
const logBufferCapacity = 64 * 1024

// ringBuffer keeps the last capacity bytes written to it. It is not a
// classic read-cursor ring buffer: there is no "read and advance"
// operation, only "write" and "snapshot everything currently held". This
// mirrors internal/webterm's ringBuffer; kept as a small separate copy
// since the two packages are otherwise independent and the type is a
// handful of lines.
type ringBuffer struct {
	mu       sync.Mutex
	buf      []byte
	capacity int
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{capacity: capacity}
}

// Write appends p to the buffer, dropping the oldest bytes once the buffer
// exceeds capacity. It never fails.
func (r *ringBuffer) Write(p []byte) {
	if len(p) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.buf = append(r.buf, p...)
	if excess := len(r.buf) - r.capacity; excess > 0 {
		r.buf = append([]byte(nil), r.buf[excess:]...)
	}
}

// Snapshot returns a copy of the currently buffered bytes.
func (r *ringBuffer) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, len(r.buf))
	copy(out, r.buf)
	return out
}
