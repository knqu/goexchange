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
	seq, offset, err := lastGood(path)
	if err != nil {
		return nil, err
	}

	// repair a torn tail (if the journal exists) by truncating to the end of the last known-good record
	// open a separate RDWR handle (because Windows can't truncate through an O_APPEND handle)
	if file, err := os.OpenFile(path, os.O_RDWR, 0o644); err == nil {
		if err := file.Truncate(offset); err != nil {
			file.Close()
			return nil, fmt.Errorf("truncating torn tail: %w", err)
		}
		file.Close()
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("opening journal to repair: %w", err)
	}

	// actually open the journal for appending
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening journal %s: %w", path, err)
	}

	bw := bufio.NewWriter(file)
	encoder := json.NewEncoder(bw)

	return &Writer{file: file, bw: bw, encoder: encoder, seq: seq}, nil
}

// lastGood scans an existing journal for the seq of the last valid record and the byte offset just past it.
// A torn final line (caused by a mid-append crash) is tolerated; its offset is excluded to be truncated by the caller.
// A torn line mid-journal (indicated by a bad line with a valid line after it) indicates real corruption and errors.
func lastGood(path string) (seq uint64, offset int64, err error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil // new journal: seq and offset both start at 0
		}
		return 0, 0, fmt.Errorf("reading journal: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		lineBytes := scanner.Bytes()
		var rec record

		if err := json.Unmarshal(lineBytes, &rec); err != nil {
			if scanner.Scan() {
				// a valid line exists after a corrupted line, meaning that the corruption was mid-file
				return 0, 0, fmt.Errorf("mid-file corruption: %w", err)
			}
			break // stop at torn final line; offset stays at last good record
		}

		seq = rec.Seq
		offset += int64(len(lineBytes)) + 1 // +1 to account for stripped newline
	}

	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}

	return seq, offset, nil
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
			if scanner.Scan() {
				// a valid line exists after a corrupted line, meaning that the corruption was mid-file
				return fmt.Errorf("journal line %d corrupt (mid-file): %w", line, err)
			}
			// otherwise, only the final line is corrupted, likely caused by the process being force killed
			return scanner.Err()
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
