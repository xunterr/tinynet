package tinynet

import (
	"io"
	"net"
)

type Stream struct {
	net.Conn
	conn    *Conn
	onClose func()
}

func newStream(s net.Conn, conn *Conn) *Stream {
	return &Stream{
		Conn:    s,
		conn:    conn,
		onClose: func() {},
	}
}

func (s *Stream) setOnClose(f func()) {
	s.onClose = f
}

func (s *Stream) Close() (err error) {
	err = s.Conn.Close()
	s.onClose()
	return
}

// Read bytes from Stream
func (s *Stream) Read(p []byte) (n int, err error) {
	n, err = s.Conn.Read(p)
	if err != nil {
		if err != io.EOF {
			s.conn.TryClose()
		}
	}
	return
}

// Write bytes to Stream
func (s *Stream) Write(p []byte) (n int, err error) {
	n, err = s.Conn.Write(p)
	if err != nil {
		s.conn.TryClose()
	}
	return
}
