package protocol

import (
	"bytes"
	"testing"
)

func TestTlv(t *testing.T) {
	messageHeader := Header{
		Version: 1,
		Type:    12,
		Length:  54,
		Tlvs: []Tlv{
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
	err := WriteHeader(buf, messageHeader)

	if err != nil {
		t.Fatalf("Failed to encode a message Header: %s", err)
	}

	decoded, err := ReadHeader(buf)
	if err != nil {
		t.Fatalf("Failed to decode a message Header: %s", err)
	}

	if len(decoded.Tlvs) != len(messageHeader.Tlvs) {
		t.Fatalf("Wrong TLVs length.")
	}
	for i, tlv := range decoded.Tlvs {
		realTlv := messageHeader.Tlvs[i]
		if !bytes.Equal(realTlv.Value, tlv.Value) || realTlv.Type != tlv.Type {
			t.Errorf("Have: %v, want: %v", realTlv, tlv)
		}
	}
}

func TestEncodeDecode(t *testing.T) {
	messageHeader := Header{
		Version: 1,
		Type:    12,
		Length:  54,
	}

	buf := bytes.NewBuffer([]byte{})
	err := WriteHeader(buf, messageHeader)

	if err != nil {
		t.Fatalf("Failed to encode a message Header: %s", err)
	}

	decoded, err := ReadHeader(buf)
	if err != nil {
		t.Fatalf("Failed to decode a message Header: %s", err)
	}

	if decoded.Version != messageHeader.Version ||
		decoded.Type != messageHeader.Type || decoded.Length != messageHeader.Length {
		t.Fatalf("Decoded values don't match original. Have: %v, want: %v", decoded, messageHeader)
	}
}
