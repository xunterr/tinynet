package internal

import (
	"encoding/binary"
	"errors"

	"github.com/xunterr/tinynet/internal/transports"
)

const MAGIC uint32 = 0xDEADBEEF

var ErrWrongMagic error = errors.New("Wrong magic bytes")

type DefaultHandshake struct {
}

func (h *DefaultHandshake) Accept(s *transports.Stream) (uint32, error) {
	bytes, err := s.ReadFull()
	if err != nil {
		return 0, err
	}

	if magic := binary.BigEndian.Uint32(bytes[:4]); magic != MAGIC {
		return 0, ErrWrongMagic
	}

	return binary.BigEndian.Uint32(bytes[4:]), nil
}

func (h *DefaultHandshake) Propose(s *transports.Stream, key uint32) error {
	var kBytes [8]byte
	binary.BigEndian.PutUint32(kBytes[:4], MAGIC)
	binary.BigEndian.PutUint32(kBytes[4:], key)
	_, err := s.Write(kBytes[:])
	return err
}
