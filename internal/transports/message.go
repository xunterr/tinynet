package transports

import (
	"encoding/binary"
	"io"
)

type messageHeader struct {
	Version uint8
	Type    uint32
	Length  uint32
}

func decodeMessageHeader(r io.Reader) (messageHeader, error) {
	bytes := make([]byte, 9)
	_, err := io.ReadFull(r, bytes)
	if err != nil {
		return messageHeader{}, err
	}

	ver := bytes[0]
	typ := binary.BigEndian.Uint32(bytes[1:5])
	length := binary.BigEndian.Uint32(bytes[5:9])

	return messageHeader{
		Version: ver,
		Type:    typ,
		Length:  length,
	}, nil
}

func encodeMessageHeader(w io.Writer, h messageHeader) error {
	var bytes [9]byte

	bytes[0] = h.Version
	binary.BigEndian.PutUint32(bytes[1:5], h.Type)
	binary.BigEndian.PutUint32(bytes[5:9], h.Length)

	_, err := w.Write(bytes[:])
	return err
}
