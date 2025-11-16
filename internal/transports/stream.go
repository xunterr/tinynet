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
	headers []tlv
	Body    []byte
}

func (m *Message) SetHeader(id byte, val []byte) {
	m.headers = append(m.headers, tlv{
		Type:  id,
		Value: val,
	})
}

// Get header by its id
// O(n) lookup
func (m *Message) GetHeader(id byte) ([]byte, bool) {
	for _, h := range m.headers {
		if h.Type == id {
			return h.Value, true
		}
	}
	return []byte{}, false
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

	return Message{
		headers: h.Tlvs,
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

// Write message bytes to Stream
func (s *Stream) Write(p []byte) (n int, err error) {
	header := header{
		Version: 0,
		Type:    0,
		Length:  uint32(len(p)),
	}
	return s.write(header, p)
}

// Write Message to Stream
func (s *Stream) WriteMessage(msg Message) (n int, err error) {
	header := header{
		Version: 0,
		Type:    0,
		Length:  uint32(len(msg.Body)),
		Tlvs:    msg.headers,
	}

	return s.write(header, msg.Body)
}

func (s *Stream) write(h header, b []byte) (n int, err error) {
	h.Length = uint32(len(b))
	err = writeHeader(s.Conn, h)
	if err != nil {
		return
	}

	return s.Conn.Write(b)
}
