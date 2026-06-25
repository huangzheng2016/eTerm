package daemon

const ringCap = 256 * 1024

type ringBuffer struct {
	buf    []byte
	start  int
	length int
}

func newRingBuffer() *ringBuffer {
	return &ringBuffer{buf: make([]byte, ringCap)}
}

func (r *ringBuffer) Write(p []byte) {
	if len(p) >= ringCap {
		copy(r.buf, p[len(p)-ringCap:])
		r.start = 0
		r.length = ringCap
		return
	}
	for _, b := range p {
		end := (r.start + r.length) % ringCap
		r.buf[end] = b
		if r.length < ringCap {
			r.length++
		} else {
			r.start = (r.start + 1) % ringCap
		}
	}
}

func (r *ringBuffer) Bytes() []byte {
	out := make([]byte, r.length)
	for i := 0; i < r.length; i++ {
		out[i] = r.buf[(r.start+i)%ringCap]
	}
	return out
}
