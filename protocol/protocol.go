package protocol

import (
	"encoding/binary"
	"errors"
	"io"
)

type Header struct {
	Version uint8
	Type    uint32
	Length  uint32
	Tlvs    []Tlv
}

const MinTlvSize = 3

type Tlv struct {
	Type  uint16
	Value []byte
}

func ReadPrefixed(r io.Reader) ([]byte, error) {
	bytes := make([]byte, 4)
	_, err := io.ReadFull(r, bytes)
	if err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(bytes)
	bytes = make([]byte, length)
	_, err = io.ReadFull(r, bytes)
	return bytes, err
}

func WritePrefixed(r io.Writer, bytes []byte) (int, error) {
	buf := make([]byte, len(bytes)+4)
	binary.BigEndian.PutUint32(buf, uint32(len(bytes)))
	copy(buf[4:], bytes)
	return r.Write(buf)
}

func ReadHeader(r io.Reader) (Header, error) {
	bytes, err := ReadPrefixed(r)
	if err != nil {
		return Header{}, err
	}
	_, h := parseHeader(bytes)
	return h, nil
}

func parseHeader(bytes []byte) (int, Header) {
	ver := bytes[0]
	typ := binary.BigEndian.Uint32(bytes[1:5])
	length := binary.BigEndian.Uint32(bytes[5:9])
	h := Header{
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

func parseTlvs(bytes []byte) (int, []Tlv) {
	est := len(bytes) / (MinTlvSize + 8) //microoptimizations <3
	tlvs := make([]Tlv, 0, est)

	var totalN int
	for len(bytes) >= MinTlvSize {
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

func parseTlv(bytes []byte) (int, Tlv, error) {
	if len(bytes) < MinTlvSize {
		return len(bytes), Tlv{}, ErrMalformedTLV
	}

	typ := binary.BigEndian.Uint16(bytes)
	length := bytes[2]

	tlvLen := int(length) + MinTlvSize

	if len(bytes) < tlvLen {
		return len(bytes), Tlv{}, ErrMalformedTLV
	}

	val := bytes[MinTlvSize:tlvLen]

	return tlvLen, Tlv{
		Type:  typ,
		Value: val,
	}, nil
}

func marshalHeader(h Header) []byte {
	tlvMaxSize := len(h.Tlvs) * (MinTlvSize + 255)
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

func marshalTlv(t Tlv, bytes []byte) int {
	length := len(t.Value)
	if length > 255 {
		length = 255
	}

	if len(bytes) < MinTlvSize+length {
		length = len(bytes) - MinTlvSize
	}

	binary.BigEndian.PutUint16(bytes, t.Type)
	bytes[2] = uint8(length)
	copy(bytes[3:], t.Value[:length])
	return MinTlvSize + length
}

func WriteHeader(w io.Writer, h Header) error {
	bytes := marshalHeader(h)
	_, err := w.Write(bytes)
	return err
}
