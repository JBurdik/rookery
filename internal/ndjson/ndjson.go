// Package ndjson is the one framer both the API socket and the attach socket
// use: one JSON object per line. Simple, human-debuggable via nc/socat, and
// having a single implementation removes a whole class of framing bugs.
package ndjson

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
)

const maxLineSize = 4 * 1024 * 1024 // 4MB — generous for a scrollback pane.read reply

// Writer serializes concurrent writers onto one connection.
type Writer struct {
	mu sync.Mutex
	w  io.Writer
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteJSON marshals v and writes it as one newline-terminated line.
func (w *Writer) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.w.Write(data)
	return err
}

// Reader reads one JSON value per line.
type Reader struct {
	sc *bufio.Scanner
}

func NewReader(r io.Reader) *Reader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLineSize)
	return &Reader{sc: sc}
}

// ReadJSON reads the next line and unmarshals it into v. Returns io.EOF when
// the connection closes cleanly.
func (r *Reader) ReadJSON(v any) error {
	if !r.sc.Scan() {
		if err := r.sc.Err(); err != nil {
			return err
		}
		return io.EOF
	}
	return json.Unmarshal(r.sc.Bytes(), v)
}
