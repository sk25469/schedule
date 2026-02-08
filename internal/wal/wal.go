package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// RecordType identifies the type of WAL record
type RecordType uint8

const (
	RecordTypeTaskCreated RecordType = iota + 1
	RecordTypeTaskCompleted
	RecordTypeTaskFailed
	RecordTypeTaskCancelled
	RecordTypeLeaseGranted
	RecordTypeLeaseExtended
	RecordTypeLeaseExpired
	RecordTypeTaskDead
)

// Record represents a WAL entry with its type and payload
type Record struct {
	Type    RecordType
	Payload interface{}
}

// Task Lifecycle Records

// TaskCreatedPayload represents creation of a new task
type TaskCreatedPayload struct {
	TaskID          string
	Payload         []byte
	ExecutionWindow time.Duration
	RetryPolicy     RetryPolicy
	RequestID       string    // optional
	CreatedAt       time.Time // optional, metadata only
}

// TaskCompletedPayload represents successful task completion
type TaskCompletedPayload struct {
	TaskID  string
	LeaseID string
}

// TaskFailedPayload represents execution failure
type TaskFailedPayload struct {
	TaskID        string
	LeaseID       string
	FailureReason string
}

// TaskCancelledPayload represents authority loss
type TaskCancelledPayload struct {
	TaskID  string
	LeaseID string
}

// TaskDeadPayload represents administrative termination
type TaskDeadPayload struct {
	TaskID string
	Reason string
}

// Lease Lifecycle Records

// LeaseGrantedPayload represents granting task ownership
type LeaseGrantedPayload struct {
	TaskID      string
	LeaseID     string
	WorkerID    string
	Attempt     int
	LeaseExpiry time.Time
	GrantedAt   time.Time // optional, metadata only
}

// LeaseExtendedPayload represents lease duration extension
type LeaseExtendedPayload struct {
	LeaseID        string
	NewLeaseExpiry time.Time
}

// LeaseExpiredPayload represents explicit lease expiration (optional)
type LeaseExpiredPayload struct {
	TaskID  string
	LeaseID string
}

// RetryPolicy defines retry behavior for tasks
type RetryPolicy struct {
	MaxRetries int
	// Add other retry policy fields as needed
}

// WAL represents the Write-Ahead Log
type WAL struct {
	mu            sync.Mutex
	file          *os.File
	filePath      string
	offset        int64
	syncBatchSize int // configurable batch size for fsync
}

// Config holds WAL configuration
type Config struct {
	FilePath      string
	SyncBatchSize int // number of records before fsync
}

// Errors
var (
	ErrWALClosed       = errors.New("wal: log is closed")
	ErrInvalidRecord   = errors.New("wal: invalid record")
	ErrCorruptedLog    = errors.New("wal: corrupted log file")
	ErrPartialWrite    = errors.New("wal: partial write detected")
	ErrInvalidChecksum = errors.New("wal: checksum mismatch")
)

// Open creates or opens a WAL file
// Returns a WAL instance ready for append and replay operations
func Open(config Config) (*WAL, error) {
	if config.SyncBatchSize <= 0 {
		config.SyncBatchSize = 1 // default: sync after every write
	}

	file, err := os.OpenFile(config.FilePath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat WAL file: %w", err)
	}

	wal := &WAL{
		file:          file,
		filePath:      config.FilePath,
		offset:        stat.Size(),
		syncBatchSize: config.SyncBatchSize,
	}

	return wal, nil
}

// Append writes a record to the WAL
// Records are buffered until Sync() is called or batch size is reached
func (w *WAL) Append(record Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return ErrWALClosed
	}

	// TODO: Encode the record as bytes
	// This should include:
	// - Record length (4 bytes)
	// - Record type (1 byte)
	// - Payload (variable)
	// - Checksum (4 bytes)

	data, err := w.encodeRecord(record)
	if err != nil {
		return fmt.Errorf("failed to encode record: %w", err)
	}

	// Write to file
	n, err := w.file.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write record: %w", err)
	}

	if n != len(data) {
		return ErrPartialWrite
	}

	w.offset += int64(n)

	// TODO: implement batched sync logic
	// For now, just note that sync should be called explicitly or after batch

	return nil
}

// Sync forces durability by calling fsync
// All records appended before this call are guaranteed to be durable
func (w *WAL) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return ErrWALClosed
	}

	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL: %w", err)
	}

	return nil
}

// Replay reads all records from the WAL and calls the apply function for each
// This is used during recovery to reconstruct coordinator state
// Replay is deterministic and sequential
func (w *WAL) Replay(applyFn func(Record) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return ErrWALClosed
	}

	// Seek to beginning
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek WAL: %w", err)
	}

	// Read and apply records one by one
	for {
		record, err := w.readNextRecord()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Partial write at end of log is tolerable
			if errors.Is(err, ErrPartialWrite) || errors.Is(err, ErrInvalidChecksum) {
				// Discard partial final record and continue
				break
			}
			return fmt.Errorf("failed to read record during replay: %w", err)
		}

		// Apply the record
		if err := applyFn(record); err != nil {
			return fmt.Errorf("failed to apply record during replay: %w", err)
		}
	}

	// Seek back to end for future appends
	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("failed to seek to end after replay: %w", err)
	}

	return nil
}

// Close closes the WAL file
// Any unflushed data should be synced before closing
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}

	// Sync before closing
	if err := w.file.Sync(); err != nil {
		w.file.Close()
		return fmt.Errorf("failed to sync before close: %w", err)
	}

	if err := w.file.Close(); err != nil {
		return fmt.Errorf("failed to close WAL: %w", err)
	}

	w.file = nil
	return nil
}

// encodeRecord serializes a record to bytes
// Format:
// - Length (4 bytes, uint32): total length excluding length field
// - Type (1 byte): record type
// - Payload (variable): serialized payload
// - Checksum (4 bytes, uint32): CRC32 of type + payload
func (w *WAL) encodeRecord(record Record) ([]byte, error) {
	// TODO: Implement proper encoding
	// This is a placeholder that needs to be implemented based on:
	// - Chosen serialization format (e.g., protobuf, JSON, custom binary)
	// - Checksum algorithm (e.g., CRC32)

	return nil, errors.New("encodeRecord not yet implemented")
}

// readNextRecord reads the next record from the current file position
func (w *WAL) readNextRecord() (Record, error) {
	// Read length prefix (4 bytes)
	var length uint32
	if err := binary.Read(w.file, binary.LittleEndian, &length); err != nil {
		return Record{}, err
	}

	// Read the rest of the record
	data := make([]byte, length)
	if _, err := io.ReadFull(w.file, data); err != nil {
		return Record{}, ErrPartialWrite
	}

	// TODO: Decode and validate the record
	// This should:
	// - Extract record type
	// - Verify checksum
	// - Deserialize payload
	// - Return the Record struct

	return Record{}, errors.New("readNextRecord not yet implemented")
}

// Helper methods for validation and invariant checking

// ValidateRecord checks if a record is well-formed
func ValidateRecord(record Record) error {
	// TODO: Implement validation logic for each record type
	// Check that required fields are present and valid
	return errors.New("ValidateRecord not yet implemented")
}

// ApplyRecord applies a record to coordinator state
// This is called during replay and ensures invariants are preserved
func ApplyRecord(record Record, state interface{}) error {
	// TODO: Implement state application logic
	// This should:
	// - Check invariants before applying
	// - Update state based on record type
	// - Validate state transitions
	// Note: The actual implementation will depend on coordinator state structure
	return errors.New("ApplyRecord not yet implemented")
}
