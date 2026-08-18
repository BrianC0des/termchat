package system

import (
	"bytes"
	"crypto/rand"
	"testing"
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
