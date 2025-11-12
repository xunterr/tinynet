package transports

import (
	"bytes"
	"testing"
)

func TestEncodeDecode(t *testing.T) {
	messageHeader := messageHeader{
		Version: 1,
		Type:    12,
		Length:  54,
	}

	buf := bytes.NewBuffer([]byte{})
	err := encodeMessageHeader(buf, messageHeader)

	if err != nil {
		t.Fatalf("Failed to encode a message header: %s", err)
	}

	decoded, err := decodeMessageHeader(buf)
	if err != nil {
		t.Fatalf("Failed to decode a message header: %s", err)
	}

	if decoded.Version != messageHeader.Version ||
		decoded.Type != messageHeader.Type || decoded.Length != messageHeader.Length {
		t.Fatalf("Decoded values don't match original. Have: %v, want: %v", decoded, messageHeader)
	}
}
