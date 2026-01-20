package tinynet

import (
	"bufio"
	"errors"
	"io"
	"net"

	"github.com/xunterr/tinynet/protocol"
)

type Header = protocol.Tlv
type Headers []protocol.Tlv

var ErrPartialRead error = errors.New("Partially read message")

type Stream struct { // <-!!!
	net.Conn
	conn    *Conn
	r       *bufio.Reader
	n       int
	onClose func()
}

func newStream(s net.Conn, conn *Conn) *Stream {
	return &Stream{
		Conn:    s,
		conn:    conn,
		r:       bufio.NewReader(s),
		n:       0,
		onClose: func() {},
	}
}

type Message struct {
	Ver     byte
	Type    uint32
	Body    []byte
	Headers Headers
}

func FromBytes(b []byte) Message {
	return Message{
		Body:    b,
		Headers: make(Headers, 0),
	}
}

// As user-space header
func AsHeader(id uint16, val []byte) Header {
	return Header{
		Type:  id,
		Value: val,
	}
}

func (h *Headers) AddHeader(id uint16, val []byte) {
	*h = append(*h, AsHeader(id, val))
}

// Get header by its key
func (h Headers) GetHeader(key uint16) ([]byte, bool) {
	for _, h := range h {
		if h.Type == key {
			return h.Value, true
		}
	}
	return []byte{}, false
}

func (s *Stream) setOnClose(f func()) {
	s.onClose = f
}

func (s *Stream) Close() (err error) {
	err = s.Conn.Close()
	s.onClose()
	return
}

// Reads at most one message from Stream.
// If p isn't large enough, reads len(p) bytes, and
// subsequent reads will read the rest of the message
func (s *Stream) Read(p []byte) (n int, err error) {
	if s.n == 0 {
		header, err := protocol.ReadHeader(s.r)
		if err != nil {
			s.conn.TryClose()
			return 0, err
		}
		s.n = int(header.Length)
	}

	right := min(len(p), s.n)
	readN, err := io.ReadFull(s.r, p[:right])
	s.n -= readN
	if err != nil {
		s.n = 0
		if err != io.ErrUnexpectedEOF {
			s.conn.TryClose()
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
		Ver:     h.Version,
		Type:    h.Type,
		Headers: h.Tlvs,
		Body:    bytes,
	}, nil
}

func (s *Stream) read() (protocol.Header, []byte, error) {
	if s.n != 0 {
		return protocol.Header{}, []byte{}, ErrPartialRead
	}

	h, err := protocol.ReadHeader(s.r)
	if err != nil {
		s.conn.TryClose()
		return protocol.Header{}, []byte{}, err
	}

	bytes := make([]byte, h.Length)
	_, err = io.ReadFull(s.r, bytes)
	if err != nil && err != io.ErrUnexpectedEOF {
		s.conn.TryClose()
		return protocol.Header{}, []byte{}, err
	}
	return h, bytes, nil
}

// Write message bytes to Stream
func (s *Stream) Write(p []byte) (n int, err error) {
	header := protocol.Header{
		Version: 0,
		Type:    0,
		Length:  uint32(len(p)),
	}
	return s.write(header, p)
}

// Write Message to Stream
func (s *Stream) WriteMessage(msg Message) (n int, err error) {
	header := protocol.Header{
		Version: msg.Ver,
		Type:    msg.Type,
		Length:  uint32(len(msg.Body)),
		Tlvs:    msg.Headers,
	}

	return s.write(header, msg.Body)
}

func (s *Stream) write(h protocol.Header, b []byte) (n int, err error) {
	h.Length = uint32(len(b))
	err = protocol.WriteHeader(s.Conn, h)
	if err != nil {
		return
	}

	return s.Conn.Write(b)
}
