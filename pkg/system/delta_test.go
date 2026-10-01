package system

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestDeltaEngine_ExactRoundTrip(t *testing.T) {
	// Base synthetic binary payload
	oldBytes := make([]byte, 1024*512) // 512 KB
	_, _ = rand.Read(oldBytes)

	// Create new binary with 90% common data and 10% modified/inserted sections
	newBytes := make([]byte, len(oldBytes)+1024*32)
	copy(newBytes, oldBytes[:1024*256]) // First 256 KB identical

	// Insert new 32 KB block in middle
	newSection := make([]byte, 1024*32)
	_, _ = rand.Read(newSection)
	copy(newBytes[1024*256:], newSection)

	// Second half from oldBytes
	copy(newBytes[1024*288:], oldBytes[1024*256:])

	// Generate delta patch
	patch, err := GenerateDelta(oldBytes, newBytes)
	if err != nil {
		t.Fatalf("GenerateDelta failed: %v", err)
	}

	t.Logf("Old Size: %d, New Size: %d, Patch Size: %d (Compression: %.1f%%)",
		len(oldBytes), len(newBytes), len(patch), float64(len(patch))/float64(len(newBytes))*100)

	// Apply delta patch
	reconstructed, err := ApplyDelta(oldBytes, patch)
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}

	if !bytes.Equal(reconstructed, newBytes) {
		t.Fatalf("Reconstructed binary does not match original newBytes!")
	}
}

func TestDeltaEngine_CorruptedSourceFails(t *testing.T) {
	oldBytes := []byte("Original base binary with standard runtime code blocks and text segments.")
	newBytes := []byte("Original base binary with updated runtime code blocks and newly added features.")

	patch, err := GenerateDelta(oldBytes, newBytes)
	if err != nil {
		t.Fatalf("GenerateDelta failed: %v", err)
	}

	// Try applying patch to modified/corrupted oldBytes
	corruptedOld := []byte("MODIFIED base binary with standard runtime code blocks and text segments.")
	_, err = ApplyDelta(corruptedOld, patch)
	if err == nil {
		t.Fatalf("Expected ApplyDelta to fail on corrupted source binary, but succeeded!")
	}
	t.Logf("Expected failure verified: %v", err)
}

// buildPatch wraps a raw instruction stream in a valid envelope for oldBytes.
// The target hash is deliberately wrong: these tests exercise parsing limits,
// which must reject the patch before the hash check is ever reached.
func buildPatch(t *testing.T, oldBytes []byte, targetSize uint64, stream []byte) []byte {
	t.Helper()
	var env bytes.Buffer
	env.WriteString(DeltaMagic)
	src := sha256.Sum256(oldBytes)
	env.Write(src[:])
	env.Write(make([]byte, 32))
	_ = binary.Write(&env, binary.LittleEndian, targetSize)
	enc, err := zstd.NewWriter(&env)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = enc.Write(stream)
	_ = enc.Close()
	return env.Bytes()
}

func le32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }

func TestApplyDeltaRejectsMaliciousPatches(t *testing.T) {
	old := bytes.Repeat([]byte("abcdefgh"), 1024)
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

	cases := map[string][]byte{
		// offset+length wraps uint32 to a small value: must not slice-panic.
		"copy overflow": buildPatch(t, old, 100, cat([]byte{opCopy}, le32(0xFFFFFFF0), le32(0x20))),
		"diff overflow": buildPatch(t, old, 100, cat([]byte{opDiff}, le32(0xFFFFFFF0), le32(0x20))),
		// 4 GB insert claim with no payload: must not allocate.
		"huge insert": buildPatch(t, old, 100, cat([]byte{opInsert}, le32(0xFFFFFFFF))),
		// Declared target absurdly large: must not make([]byte, 0, huge).
		"huge target": buildPatch(t, old, 1<<62, cat([]byte{opInsert}, le32(1), []byte{1})),
		"zero target": buildPatch(t, old, 0, nil),
		// Output larger than declared size via repeated copies.
		"output overrun": buildPatch(t, old, 100, bytes.Repeat(cat([]byte{opCopy}, le32(0), le32(64)), 50)),
	}
	for name, patch := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ApplyDelta panicked: %v", r)
				}
			}()
			if _, err := ApplyDelta(old, patch); err == nil {
				t.Fatal("malicious patch was accepted")
			}
		})
	}
}

func TestApplyDeltaStreamBombRejected(t *testing.T) {
	old := bytes.Repeat([]byte("x"), 4096)
	// 8 MB of zero opcodes compresses to a few KB but far exceeds the
	// stream allowance for a 100-byte target.
	patch := buildPatch(t, old, 100, make([]byte, 8<<20))
	if _, err := ApplyDelta(old, patch); err == nil || !strings.Contains(err.Error(), "exceeds allowed size") {
		t.Fatalf("expected stream-size rejection, got %v", err)
	}
}
