package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/knqu/goexchange/internal/engine"
)

// record is a single journal entry, consisting of a sequence number and the command that was executed.
type record struct {
	Seq uint64         `json:"seq"`
	Cmd engine.Command `json:"cmd"`
}

// Writer appends command records serialized as JSON to a journal file.
type Writer struct {
	file *os.File
	bw   *bufio.Writer // buffer bytes in memory instead of immediately writing every record to the disk
	enc  *json.Encoder // encode go values as json bytes
	seq  uint64
}

// NewWriter opens a journal file for appending, creating it if it doesn't exist, and returns a Writer.
func NewWriter(path string) (*Writer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening journal %s: %w", path, err)
	}

	bw := bufio.NewWriter(file)
	enc := json.NewEncoder(bw)

	return &Writer{file: file, bw: bw, enc: enc}, nil
}

// Append writes a new record to the journal file, with a newline automatically added by the encoder.
func (w *Writer) Append(cmd engine.Command) error {
	w.seq++

	if err := w.enc.Encode(record{Seq: w.seq, Cmd: cmd}); err != nil {
		return fmt.Errorf("journal append: %w", err)
	}

	return nil
}

// Sync flushes buffered records and asks the OS to flush pending writes to the disk.
func (w *Writer) Sync() error {
	if err := w.bw.Flush(); err != nil {
		return err
	}
	return w.file.Sync()
}

// Close closes the journal file after syncing any buffered records to the disk.
func (w *Writer) Close() error {
	if err := w.Sync(); err != nil {
		return err
	}
	return w.file.Close()
}
