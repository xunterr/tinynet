package internal

import (
	"encoding/binary"
	"errors"
)

const MAGIC uint32 = 0xDEADBEEF

var ErrWrongMagic error = errors.New("Wrong magic bytes")

type DefaultHandshake struct {
}

func (h *DefaultHandshake) Accept(s *Stream) ([]Header, error) {
	msg, err := s.ReadMessage()
	if err != nil {
		return []Header{}, err
	}

	if magic := binary.BigEndian.Uint32(msg.Body[:4]); magic != MAGIC {
		return []Header{}, ErrWrongMagic
	}

	return msg.Headers, nil
}

func (h *DefaultHandshake) Propose(s *Stream, headers []Header) error {
	var kBytes [4]byte
	binary.BigEndian.PutUint32(kBytes[:4], MAGIC)
	msg := Message{
		Headers: headers,
		Body:    kBytes[:],
	}
	_, err := s.WriteMessage(msg)
	return err
}
