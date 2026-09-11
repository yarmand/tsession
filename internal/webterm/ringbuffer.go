package webterm

import "sync"

// ringBuffer keeps the last capacity bytes written to it, so a reconnecting
// client can replay recent terminal output instead of seeing a blank
// screen. It is not a classic read-cursor ring buffer: there is no "read and
// advance" operation, only "write" and "snapshot everything currently held".
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
