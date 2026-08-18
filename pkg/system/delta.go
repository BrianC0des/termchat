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
// It uses block-hash matching, substring expansion, byte-level diffing, and Zstandard compression.
func GenerateDelta(oldBytes, newBytes []byte) ([]byte, error) {
	sourceHash := sha256.Sum256(oldBytes)
	targetHash := sha256.Sum256(newBytes)

	// 1. Build 16-byte sliding window hash index on oldBytes
	oldLen := len(oldBytes)
	newLen := len(newBytes)

	index := make(map[uint32][]int)
	if oldLen >= deltaBlockSize {
		for i := 0; i <= oldLen-deltaBlockSize; i += 4 {
			h := binary.LittleEndian.Uint32(oldBytes[i : i+4])
			index[h] = append(index[h], i)
		}
	}

	var rawStream bytes.Buffer
	newPos := 0

	for newPos < newLen {
		bestOldPos := -1
		bestMatchLen := 0

		if newPos <= newLen-deltaBlockSize && oldLen >= deltaBlockSize {
			h := binary.LittleEndian.Uint32(newBytes[newPos : newPos+4])
			if candidates, found := index[h]; found {
				for _, oldPos := range candidates {
					// Count exact matching bytes
					matchLen := 0
					for oldPos+matchLen < oldLen && newPos+matchLen < newLen && oldBytes[oldPos+matchLen] == newBytes[newPos+matchLen] {
						matchLen++
					}
					if matchLen > bestMatchLen {
						bestMatchLen = matchLen
						bestOldPos = oldPos
					}
				}
			}
		}

		// If we found a significant matching block (>= 16 bytes)
		if bestMatchLen >= deltaBlockSize {
			// Check if we can extend with small byte-level differences (Courgette style diffing)
			diffLen := bestMatchLen
			for bestOldPos+diffLen < oldLen && newPos+diffLen < newLen && diffLen < 65535 {
				diffLen++
			}

			// If exact match is long, write OpCopy
			if bestMatchLen >= 24 {
				rawStream.WriteByte(opCopy)
				_ = binary.Write(&rawStream, binary.LittleEndian, uint32(bestOldPos))
				_ = binary.Write(&rawStream, binary.LittleEndian, uint32(bestMatchLen))
				newPos += bestMatchLen
				continue
			}
		}

		// If no good match, accumulate literal bytes for OpInsert
		insertStart := newPos
		for newPos < newLen {
			if newPos <= newLen-deltaBlockSize {
				h := binary.LittleEndian.Uint32(newBytes[newPos : newPos+4])
				if candidates, found := index[h]; found && len(candidates) > 0 {
					// Check if there is a match of >= 24 bytes
					hasGoodMatch := false
					for _, op := range candidates {
						mLen := 0
						for op+mLen < oldLen && newPos+mLen < newLen && oldBytes[op+mLen] == newBytes[newPos+mLen] {
							mLen++
						}
						if mLen >= 24 {
							hasGoodMatch = true
							break
						}
					}
					if hasGoodMatch {
						break
					}
				}
			}
			newPos++
		}

		insertLen := newPos - insertStart
		if insertLen > 0 {
			rawStream.WriteByte(opInsert)
			_ = binary.Write(&rawStream, binary.LittleEndian, uint32(insertLen))
			rawStream.Write(newBytes[insertStart:newPos])
		}
	}

	// 2. Compress the instruction stream using Zstd with best compression
	var compressedData bytes.Buffer
	enc, err := zstd.NewWriter(&compressedData, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
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

	// 3. Assemble binary envelope:
	// [0..3]   Magic "TCD1"
	// [4..35]  Source SHA-256
	// [36..67] Target SHA-256
	// [68..75] Target Size (uint64)
	// [76..]   Compressed payload
	var envelope bytes.Buffer
	envelope.WriteString(DeltaMagic)
	envelope.Write(sourceHash[:])
	envelope.Write(targetHash[:])
	_ = binary.Write(&envelope, binary.LittleEndian, uint64(newLen))
	envelope.Write(compressedData.Bytes())

	return envelope.Bytes(), nil
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

	// 3. Decompress instruction stream
	compressedPayload := patchEnvelope[76:]
	dec, err := zstd.NewReader(bytes.NewReader(compressedPayload))
	if err != nil {
		return nil, fmt.Errorf("zstd reader init failed: %w", err)
	}
	defer dec.Close()

	decompressedStream, err := io.ReadAll(dec)
	if err != nil {
		return nil, fmt.Errorf("zstd decompression failed: %w", err)
	}

	// 4. Reconstruct target binary
	out := make([]byte, 0, targetSize)
	reader := bytes.NewReader(decompressedStream)
	oldLen := len(oldBytes)

	for reader.Len() > 0 {
		op, err := reader.ReadByte()
		if err != nil {
			break
		}

		switch op {
		case opCopy:
			var oldOffset, length uint32
			if err := binary.Read(reader, binary.LittleEndian, &oldOffset); err != nil {
				return nil, fmt.Errorf("corrupted opCopy offset: %w", err)
			}
			if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
				return nil, fmt.Errorf("corrupted opCopy length: %w", err)
			}
			if int(oldOffset+length) > oldLen {
				return nil, fmt.Errorf("opCopy out of bounds: offset=%d len=%d oldLen=%d", oldOffset, length, oldLen)
			}
			out = append(out, oldBytes[oldOffset:oldOffset+length]...)

		case opInsert:
			var length uint32
			if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
				return nil, fmt.Errorf("corrupted opInsert length: %w", err)
			}
			buf := make([]byte, length)
			if _, err := io.ReadFull(reader, buf); err != nil {
				return nil, fmt.Errorf("corrupted opInsert data: %w", err)
			}
			out = append(out, buf...)

		case opDiff:
			var oldOffset, length uint32
			if err := binary.Read(reader, binary.LittleEndian, &oldOffset); err != nil {
				return nil, fmt.Errorf("corrupted opDiff offset: %w", err)
			}
			if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
				return nil, fmt.Errorf("corrupted opDiff length: %w", err)
			}
			if int(oldOffset+length) > oldLen {
				return nil, fmt.Errorf("opDiff out of bounds: offset=%d len=%d oldLen=%d", oldOffset, length, oldLen)
			}
			diffBuf := make([]byte, length)
			if _, err := io.ReadFull(reader, diffBuf); err != nil {
				return nil, fmt.Errorf("corrupted opDiff data: %w", err)
			}
			for i := uint32(0); i < length; i++ {
				out = append(out, oldBytes[oldOffset+i]+diffBuf[i])
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
