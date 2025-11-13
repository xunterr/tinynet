package transports

import (
	"bytes"
	"testing"
)

func BenchmarkEncodeDecode(b *testing.B) {
	messageHeader := header{
		Version: 1,
		Type:    12,
		Length:  54,
		Tlvs: []tlv{
			{Type: 1, Value: []byte{1, 2, 3, 4, 5, 6, 7}},
			{Type: 2, Value: []byte{1, 2, 4, 4, 5, 6, 7}},
			{Type: 3, Value: []byte{1, 2, 1, 4, 5, 6, 7}},
			{Type: 4, Value: []byte{1, 2, 7, 4, 5, 6, 7}},
			{Type: 5, Value: []byte{1, 2, 0, 4, 5, 6, 7}},
			{Type: 5, Value: []byte{1, 2, 0, 4, 5, 6, 7}},
			{Type: 5, Value: []byte{1, 2, 0, 4, 5, 6, 7}},
			{Type: 5, Value: []byte{1, 2, 0, 4, 5, 6, 7}},
		},
	}

	buf := bytes.NewBuffer([]byte{})
	err := writeHeader(buf, messageHeader)
	r := bytes.NewReader(nil)

	if err != nil {
		b.Fatalf("Failed to encode a message header: %s", err)
	}

	data := buf.Bytes()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r.Reset(data)
		if _, err := readHeader(r); err != nil {
			b.Fatal(err)
		}
	}
}

func TestEncodeDecode(t *testing.T) {
	messageHeader := header{
		Version: 1,
		Type:    12,
		Length:  54,
	}

	buf := bytes.NewBuffer([]byte{})
	err := writeHeader(buf, messageHeader)

	if err != nil {
		t.Fatalf("Failed to encode a message header: %s", err)
	}

	decoded, err := readHeader(buf)
	if err != nil {
		t.Fatalf("Failed to decode a message header: %s", err)
	}

	if decoded.Version != messageHeader.Version ||
		decoded.Type != messageHeader.Type || decoded.Length != messageHeader.Length {
		t.Fatalf("Decoded values don't match original. Have: %v, want: %v", decoded, messageHeader)
	}
}
