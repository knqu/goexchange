package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/knqu/goexchange/internal/engine"
)

// --- journal data structures ---

// record is a single journal entry, consisting of a sequence number and the command that was executed.
type record struct {
	Seq uint64         `json:"seq"`
	Cmd engine.Command `json:"cmd"`
}

// --- journal writer ---

// Writer appends command records serialized as JSON to a journal file.
type Writer struct {
	file    *os.File
	bw      *bufio.Writer // buffer bytes in memory instead of immediately writing every record to the disk
	encoder *json.Encoder // encode go values as json bytes
	seq     uint64
}

// NewWriter opens a journal file for appending and returns a Writer (with seq initialized or restored from existing).
func NewWriter(path string) (*Writer, error) {
	seq, err := lastSeq(path)
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening journal %s: %w", path, err)
	}

	bw := bufio.NewWriter(file)
	encoder := json.NewEncoder(bw)

	return &Writer{file: file, bw: bw, encoder: encoder, seq: seq}, nil
}

// lastSeq returns the seq of the final record in an existing journal, or 0 if the file does not exist or is empty.
func lastSeq(path string) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // new journal; start from 0
		}
		return 0, fmt.Errorf("reading journal for seq: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var seq uint64

	for scanner.Scan() {
		var rec record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return 0, fmt.Errorf("parsing journal record: %w", err)
		}
		seq = rec.Seq
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return seq, nil // returns 0 if file exists but is empty
}

// Append writes a new record to the journal file, with a newline automatically added by the encoder.
func (w *Writer) Append(cmd engine.Command) error {
	w.seq++

	if err := w.encoder.Encode(record{Seq: w.seq, Cmd: cmd}); err != nil {
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

// --- journal reader ---

// Replay streams every journaled command in order into fn.
func Replay(path string, fn func(engine.Command) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening journal for replay: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // lift scanner's default per-line ceiling to allow long lines
	var lastSeq uint64
	line := 0

	for scanner.Scan() {
		line++

		var rec record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return fmt.Errorf("journal line %d corrupt: %w", line, err)
		}

		if line > 1 && rec.Seq != lastSeq+1 {
			return fmt.Errorf("journal line %d: seq %d follows %d (expected %d)", line, rec.Seq, lastSeq, lastSeq+1)
		}
		lastSeq = rec.Seq

		if err := fn(rec.Cmd); err != nil {
			return fmt.Errorf("replaying line %d: %w", line, err)
		}
	}

	return scanner.Err()
}
