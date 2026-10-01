package system

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

const (
	// DeltaMagic is the 4-byte header identifier for TermChat Delta patches ("TCD1")
	DeltaMagic = "TCD1"
	// BlockSize is the sliding window hash chunk size for delta matching
	deltaBlockSize = 16
)

const (
	opCopy   byte = 1
	opInsert byte = 2
	opDiff   byte = 3
)

// DeltaPatch represents a generated binary delta patch
type DeltaPatch struct {
	SourceSHA256 [32]byte
	TargetSHA256 [32]byte
	TargetSize   uint64
	Data         []byte // Zstd compressed delta instructions and payload
}

// GenerateDelta computes a binary delta patch from oldBytes to newBytes.
// It uses fast sliding block-hash matching and Zstandard compression in O(N) linear time.
func GenerateDelta(oldBytes, newBytes []byte) ([]byte, error) {
	sourceHash := sha256.Sum256(oldBytes)
	targetHash := sha256.Sum256(newBytes)

	oldLen := len(oldBytes)
	newLen := len(newBytes)

	// 1. Build fast 4-byte hash index on oldBytes (stride 4)
	index := make(map[uint32]int)
	if oldLen >= 4 {
		for i := 0; i <= oldLen-4; i += 4 {
			h := binary.LittleEndian.Uint32(oldBytes[i : i+4])
			index[h] = i
		}
	}

	var rawStream bytes.Buffer
	newPos := 0
	insertStart := 0

	for newPos < newLen {
		if newPos <= newLen-4 && oldLen >= 4 {
			h := binary.LittleEndian.Uint32(newBytes[newPos : newPos+4])
			if oldPos, found := index[h]; found {
				// Count matching length starting at oldPos and newPos
				matchLen := 0
				for oldPos+matchLen < oldLen && newPos+matchLen < newLen && oldBytes[oldPos+matchLen] == newBytes[newPos+matchLen] {
					matchLen++
				}

				if matchLen >= 16 {
					// Emit accumulated insert literals if any
					if newPos > insertStart {
						insertLen := newPos - insertStart
						rawStream.WriteByte(opInsert)
						_ = binary.Write(&rawStream, binary.LittleEndian, uint32(insertLen))
						rawStream.Write(newBytes[insertStart:newPos])
					}

					// Emit copy instruction
					rawStream.WriteByte(opCopy)
					_ = binary.Write(&rawStream, binary.LittleEndian, uint32(oldPos))
					_ = binary.Write(&rawStream, binary.LittleEndian, uint32(matchLen))

					newPos += matchLen
					insertStart = newPos
					continue
				}
			}
		}
		newPos++
	}

	// Emit trailing insert literals if any
	if newPos > insertStart {
		insertLen := newPos - insertStart
		rawStream.WriteByte(opInsert)
		_ = binary.Write(&rawStream, binary.LittleEndian, uint32(insertLen))
		rawStream.Write(newBytes[insertStart:newPos])
	}

	// 2. Compress the instruction stream using Zstd
	var compressedData bytes.Buffer
	enc, err := zstd.NewWriter(&compressedData, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("zstd encoder init failed: %w", err)
	}
	if _, err := enc.Write(rawStream.Bytes()); err != nil {
		enc.Close()
		return nil, fmt.Errorf("zstd compression failed: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("zstd close failed: %w", err)
	}

	// 3. Serialize patch header
	var patch bytes.Buffer
	patch.WriteString(DeltaMagic)
	patch.Write(sourceHash[:])
	patch.Write(targetHash[:])
	_ = binary.Write(&patch, binary.LittleEndian, uint64(newLen))
	patch.Write(compressedData.Bytes())

	return patch.Bytes(), nil
}

// ApplyDelta applies a delta patch envelope to oldBytes and reconstructs newBytes with SHA-256 validation.
func ApplyDelta(oldBytes, patchEnvelope []byte) ([]byte, error) {
	if len(patchEnvelope) < 76 {
		return nil, fmt.Errorf("delta patch corrupted: payload too small (%d bytes)", len(patchEnvelope))
	}

	// 1. Verify Magic
	magic := string(patchEnvelope[0:4])
	if magic != DeltaMagic {
		return nil, fmt.Errorf("invalid delta patch magic header: '%s' (expected '%s')", magic, DeltaMagic)
	}

	var expectedSourceHash [32]byte
	copy(expectedSourceHash[:], patchEnvelope[4:36])

	var expectedTargetHash [32]byte
	copy(expectedTargetHash[:], patchEnvelope[36:68])

	targetSize := binary.LittleEndian.Uint64(patchEnvelope[68:76])

	// 2. Verify Source Binary SHA-256
	actualSourceHash := sha256.Sum256(oldBytes)
	if actualSourceHash != expectedSourceHash {
		return nil, fmt.Errorf("source binary mismatch (hash %x != expected %x) — base version modified", actualSourceHash[:8], expectedSourceHash[:8])
	}

	// The header is attacker-controlled (only the mirror vouches for it), so
	// bound everything derived from it before allocating or looping.
	if targetSize == 0 || targetSize > uint64(maxUpdateExtracted) {
		return nil, fmt.Errorf("delta target size %d outside allowed range", targetSize)
	}

	// 3. Decompress instruction stream, bounded (decompression-bomb guard).
	// A legitimate stream is at most the literal payload plus 9 bytes per op.
	maxStream := int64(targetSize) + int64(targetSize)/2 + (1 << 20)
	compressedPayload := patchEnvelope[76:]
	dec, err := zstd.NewReader(bytes.NewReader(compressedPayload))
	if err != nil {
		return nil, fmt.Errorf("zstd reader init failed: %w", err)
	}
	defer dec.Close()

	decompressedStream, err := io.ReadAll(io.LimitReader(dec, maxStream+1))
	if err != nil {
		return nil, fmt.Errorf("zstd decompression failed: %w", err)
	}
	if int64(len(decompressedStream)) > maxStream {
		return nil, fmt.Errorf("delta instruction stream exceeds allowed size")
	}

	// 4. Reconstruct target binary. All arithmetic is done in uint64 so
	// offset+length cannot wrap, and output never exceeds targetSize.
	out := make([]byte, 0, targetSize)
	reader := bytes.NewReader(decompressedStream)
	oldLen := uint64(len(oldBytes))

	room := func(n uint64) error {
		if uint64(len(out))+n > targetSize {
			return fmt.Errorf("delta output exceeds declared target size %d", targetSize)
		}
		return nil
	}
	readRange := func(name string) (off, length uint32, err error) {
		if err = binary.Read(reader, binary.LittleEndian, &off); err != nil {
			return 0, 0, fmt.Errorf("corrupted %s offset: %w", name, err)
		}
		if err = binary.Read(reader, binary.LittleEndian, &length); err != nil {
			return 0, 0, fmt.Errorf("corrupted %s length: %w", name, err)
		}
		if uint64(off)+uint64(length) > oldLen {
			return 0, 0, fmt.Errorf("%s out of bounds: offset=%d len=%d oldLen=%d", name, off, length, oldLen)
		}
		if err = room(uint64(length)); err != nil {
			return 0, 0, err
		}
		return off, length, nil
	}

	for reader.Len() > 0 {
		op, err := reader.ReadByte()
		if err != nil {
			break
		}

		switch op {
		case opCopy:
			off, length, err := readRange("opCopy")
			if err != nil {
				return nil, err
			}
			out = append(out, oldBytes[off:uint64(off)+uint64(length)]...)

		case opInsert:
			var length uint32
			if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
				return nil, fmt.Errorf("corrupted opInsert length: %w", err)
			}
			if uint64(length) > uint64(reader.Len()) {
				return nil, fmt.Errorf("corrupted opInsert data: length %d exceeds remaining %d", length, reader.Len())
			}
			if err := room(uint64(length)); err != nil {
				return nil, err
			}
			start := len(decompressedStream) - reader.Len()
			out = append(out, decompressedStream[start:start+int(length)]...)
			_, _ = reader.Seek(int64(length), io.SeekCurrent)

		case opDiff:
			off, length, err := readRange("opDiff")
			if err != nil {
				return nil, err
			}
			if uint64(length) > uint64(reader.Len()) {
				return nil, fmt.Errorf("corrupted opDiff data: length %d exceeds remaining %d", length, reader.Len())
			}
			diffBuf := make([]byte, length)
			if _, err := io.ReadFull(reader, diffBuf); err != nil {
				return nil, fmt.Errorf("corrupted opDiff data: %w", err)
			}
			for i := uint32(0); i < length; i++ {
				out = append(out, oldBytes[off+i]+diffBuf[i])
			}

		default:
			return nil, fmt.Errorf("unknown delta opcode: 0x%02x", op)
		}
	}

	if uint64(len(out)) != targetSize {
		return nil, fmt.Errorf("reconstructed binary size mismatch: got %d bytes, expected %d bytes", len(out), targetSize)
	}

	// 5. Verify Target Binary SHA-256
	actualTargetHash := sha256.Sum256(out)
	if actualTargetHash != expectedTargetHash {
		return nil, fmt.Errorf("target binary SHA-256 verification failed (got %x, expected %x)", actualTargetHash, expectedTargetHash)
	}

	return out, nil
}
