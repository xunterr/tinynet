package transports

import (
	"errors"
	"io"
	"net"
)

var ErrPartialRead error = errors.New("Partially read message")

type Stream struct { // <-!!!
	net.Conn
	n int
}

func newStream(c net.Conn) *Stream {
	return &Stream{
		Conn: c,
		n:    0,
	}
}

type Message struct {
	Headers map[uint8][]byte
	Body    []byte
}

// Reads at most one message from Stream.
// If p isn't large enough, reads len(p) bytes, and
// subsequent reads will read the rest of the message
func (s *Stream) Read(p []byte) (n int, err error) {
	if s.n == 0 {
		header, err := readHeader(s.Conn)
		if err != nil {
			return 0, err
		}
		s.n = int(header.Length)
	}

	right := min(len(p), s.n)
	readN, err := io.ReadFull(s.Conn, p[:right])
	s.n -= readN
	if err != nil {
		s.n = 0
		if err != io.ErrUnexpectedEOF {
			return 0, err
		}
	}
	return readN, nil
}

// Reads at most one message body from Stream.
// ErrPartialRead if there is a partially read message
func (s *Stream) ReadFull() ([]byte, error) {
	_, b, err := s.read()
	return b, err
}

// Reads at most one Message from Stream.
// ErrPartialRead if there is a partially read message
func (s *Stream) ReadMessage() (Message, error) {
	h, bytes, err := s.read()
	if err != nil {
		return Message{}, err
	}

	headers := make(map[uint8][]byte, len(h.Tlvs))
	for _, t := range h.Tlvs {
		headers[t.Type] = t.Value
	}
	return Message{
		Headers: headers,
		Body:    bytes,
	}, nil
}

func (s *Stream) read() (header, []byte, error) {
	if s.n != 0 {
		return header{}, []byte{}, ErrPartialRead
	}

	h, err := readHeader(s.Conn)
	if err != nil {
		return header{}, []byte{}, err
	}

	bytes := make([]byte, h.Length)
	_, err = io.ReadFull(s.Conn, bytes)
	if err != nil && err != io.ErrUnexpectedEOF {
		return header{}, []byte{}, err
	}
	return h, bytes, nil
}

// Writes message to Stream
func (s *Stream) Write(p []byte) (n int, err error) {
	header := header{
		Version: 0,
		Type:    0,
		Length:  uint32(len(p)),
	}
	err = writeHeader(s.Conn, header)
	if err != nil {
		return
	}

	return s.Conn.Write(p)
}
