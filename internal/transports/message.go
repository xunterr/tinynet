package transports

import (
	"encoding/binary"
	"errors"
	"io"
)

type header struct {
	Version uint8
	Type    uint32
	Length  uint32
	Tlvs    []tlv
}

const MIN_TLV_SIZE = 2

type tlv struct {
	Type  uint8
	Value []byte
}

func readHeader(r io.Reader) (header, error) {
	bytes := make([]byte, 4)
	_, err := io.ReadFull(r, bytes)
	if err != nil {
		return header{}, err
	}

	length := binary.BigEndian.Uint32(bytes)
	bytes = make([]byte, length)
	_, err = io.ReadFull(r, bytes)
	if err != nil {
		return header{}, err
	}

	_, h := parseHeader(bytes)
	return h, nil
}

func parseHeader(bytes []byte) (int, header) {
	ver := bytes[0]
	typ := binary.BigEndian.Uint32(bytes[1:5])
	length := binary.BigEndian.Uint32(bytes[5:9])
	h := header{
		Version: ver,
		Type:    typ,
		Length:  length,
	}

	var n int
	if len(bytes) > 9 {
		n, h.Tlvs = parseTlvs(bytes[9:])
	}

	return n, h
}

func parseTlvs(bytes []byte) (int, []tlv) {
	est := len(bytes) / (MIN_TLV_SIZE + 8)
	tlvs := make([]tlv, 0, est)

	var totalN int
	for len(bytes) >= MIN_TLV_SIZE {
		n, tlv, err := parseTlv(bytes[:])
		if err == nil {
			tlvs = append(tlvs, tlv)
		}
		bytes = bytes[n:]
		totalN += n
	}

	return totalN, tlvs
}

var ErrMalformedTLV error = errors.New("Malformed TLV")

func parseTlv(bytes []byte) (int, tlv, error) {
	if len(bytes) < MIN_TLV_SIZE {
		return len(bytes), tlv{}, ErrMalformedTLV
	}

	typ := bytes[0]
	length := bytes[1]

	tlvLen := int(length) + MIN_TLV_SIZE

	if len(bytes) < tlvLen {
		return len(bytes), tlv{}, ErrMalformedTLV
	}

	val := bytes[2:tlvLen]

	return tlvLen, tlv{
		Type:  typ,
		Value: val,
	}, nil
}

func marshalHeader(h header) []byte {
	tlvMaxSize := len(h.Tlvs) * (MIN_TLV_SIZE + 255)
	bytes := make([]byte, 13+tlvMaxSize)
	bytes[4] = h.Version
	binary.BigEndian.PutUint32(bytes[5:9], h.Type)
	binary.BigEndian.PutUint32(bytes[9:13], h.Length)

	n := 13
	for _, t := range h.Tlvs {
		n += marshalTlv(t, bytes[n:])
	}

	bytes = bytes[:n]
	binary.BigEndian.PutUint32(bytes, uint32(len(bytes)-4))

	return bytes
}

func marshalTlv(t tlv, bytes []byte) int {
	length := len(t.Value)
	if length > 255 {
		length = 255
	}

	if len(bytes) < MIN_TLV_SIZE+length {
		length = len(bytes) - MIN_TLV_SIZE
	}

	bytes[0] = t.Type
	bytes[1] = uint8(length)
	copy(bytes[2:], t.Value[:length])
	return MIN_TLV_SIZE + length
}

func writeHeader(w io.Writer, h header) error {
	bytes := marshalHeader(h)
	_, err := w.Write(bytes)
	return err
}
