package audiosocket

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Message types for AudioSocket protocol
const (
	MsgTypeHangup = 0x00
	MsgTypeUUID   = 0x01
	MsgTypeAudio  = 0x10
	MsgTypeError  = 0xFF
)

// Handler is called for each new AudioSocket connection from Asterisk
type Handler func(ctx context.Context, conn *Connection)

// Connection wraps a single AudioSocket TCP connection
type Connection struct {
	conn   net.Conn
	uuid   string
	mu     sync.Mutex
	closed bool
	logger *zap.Logger
}

// Server listens for AudioSocket connections from Asterisk
type Server struct {
	addr     string
	listener net.Listener
	handler  Handler
	logger   *zap.Logger
	wg       sync.WaitGroup
}

func NewServer(addr string, handler Handler, logger *zap.Logger) *Server {
	return &Server{
		addr:    addr,
		handler: handler,
		logger:  logger,
	}
}

func (s *Server) Start(ctx context.Context) error {
	var err error
	s.listener, err = net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("audiosocket listen on %s: %w", s.addr, err)
	}
	s.logger.Info("AudioSocket server started", zap.String("addr", s.addr))

	go func() {
		<-ctx.Done()
		s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				s.logger.Info("AudioSocket server shutting down")
				s.wg.Wait()
				return nil
			default:
				s.logger.Error("AudioSocket accept error", zap.Error(err))
				continue
			}
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(ctx, conn)
		}()
	}
}

func (s *Server) handleConn(ctx context.Context, rawConn net.Conn) {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer rawConn.Close()

	c := &Connection{
		conn:   rawConn,
		logger: s.logger,
	}

	// First message should be UUID
	uuid, err := c.readUUID()
	if err != nil {
		s.logger.Error("Failed to read UUID from AudioSocket", zap.Error(err))
		return
	}
	c.uuid = uuid
	s.logger.Info("AudioSocket connection established",
		zap.String("uuid", uuid),
		zap.String("remote", rawConn.RemoteAddr().String()))

	s.handler(connCtx, c)
}

// UUID returns the call UUID sent by Asterisk
func (c *Connection) UUID() string {
	return c.uuid
}

// ReadAudio reads the next audio frame from Asterisk
// Returns the raw PCM audio bytes, or error
func (c *Connection) ReadAudio() ([]byte, error) {
	for {
		msgType, payload, err := c.readMessage()
		if err != nil {
			return nil, err
		}
		switch msgType {
		case MsgTypeAudio:
			return payload, nil
		case MsgTypeHangup:
			return nil, io.EOF
		case MsgTypeError:
			return nil, fmt.Errorf("audiosocket error from Asterisk: %x", payload)
		case MsgTypeUUID:
			// Already handled, ignore duplicates
			continue
		default:
			c.logger.Warn("Unknown AudioSocket message type", zap.Uint8("type", msgType))
			continue
		}
	}
}

// WriteAudio sends audio back to Asterisk (TTS from AI)
func (c *Connection) WriteAudio(pcm []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("connection closed")
	}
	return c.writeMessage(MsgTypeAudio, pcm)
}

// Close gracefully closes the AudioSocket connection
func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	// Send hangup message
	_ = c.writeMessage(MsgTypeHangup, nil)
	return c.conn.Close()
}

// --- Low-level protocol ---

// AudioSocket protocol:
// [1 byte type] [2 bytes length (network order)] [N bytes payload]
func (c *Connection) readMessage() (uint8, []byte, error) {
	c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	header := make([]byte, 3)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return 0, nil, err
	}

	msgType := header[0]
	length := binary.BigEndian.Uint16(header[1:3])

	if length == 0 {
		return msgType, nil, nil
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return 0, nil, fmt.Errorf("reading payload: %w", err)
	}

	return msgType, payload, nil
}

func (c *Connection) writeMessage(msgType uint8, payload []byte) error {
	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

	header := make([]byte, 3)
	header[0] = msgType
	binary.BigEndian.PutUint16(header[1:3], uint16(len(payload)))

	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := c.conn.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func (c *Connection) readUUID() (string, error) {
	msgType, payload, err := c.readMessage()
	if err != nil {
		return "", fmt.Errorf("reading UUID message: %w", err)
	}
	if msgType != MsgTypeUUID {
		return "", fmt.Errorf("expected UUID message (0x01), got 0x%02x", msgType)
	}
	if len(payload) != 16 {
		return "", fmt.Errorf("UUID payload should be 16 bytes, got %d", len(payload))
	}
	// Format as standard UUID string
	uuid := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		payload[0:4], payload[4:6], payload[6:8], payload[8:10], payload[10:16])
	return uuid, nil
}
