package fat32

import (
	"fmt"
	"io"
	"testing"
)

type writerAtBuffer struct {
	data []byte
}

func (w writerAtBuffer) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(p)) > int64(len(w.data)) {
		return 0, io.ErrShortBuffer
	}

	copy(w.data[off:], p)

	return len(p), nil
}

type failingWriterAt struct {
	data         []byte
	failAfter    int
	writes       int
	partialWrite bool
}

func (w *failingWriterAt) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(p)) > int64(len(w.data)) {
		return 0, io.ErrShortBuffer
	}

	if w.writes >= w.failAfter {
		if w.partialWrite {
			n := len(p) / 2
			copy(w.data[off:], p[:n])
			w.writes++

			return n, fmt.Errorf("injected partial write failure")
		}

		return 0, fmt.Errorf("injected write failure")
	}

	copy(w.data[off:], p)
	w.writes++

	return len(p), nil
}

func TestFailingWriterAtPartialWrite(t *testing.T) {
	data := make([]byte, 16)

	writer := &failingWriterAt{
		data:         data,
		failAfter:    0,
		partialWrite: true,
	}

	input := []byte{1, 2, 3, 4}

	n, err := writer.WriteAt(input, 4)
	if err == nil {
		t.Fatal("WriteAt unexpectedly succeeded")
	}

	if n != len(input)/2 {
		t.Fatalf("bytes written = %d, want %d", n, len(input)/2)
	}

	for i, want := range input[:n] {
		if got := data[4+i]; got != want {
			t.Fatalf(
				"data[%d] = %#x, want %#x",
				4+i,
				got,
				want,
			)
		}
	}
}
