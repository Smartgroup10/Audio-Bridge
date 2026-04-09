package ami

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Client manages the AMI connection to Asterisk.
// Uses async reader goroutine + ActionID dispatch for concurrent access.
type Client struct {
	host     string
	port     int
	user     string
	password string

	conn   net.Conn
	reader *bufio.Reader
	logger *zap.Logger

	// Write serialization — only protects conn.Write, NOT read
	writeMu sync.Mutex

	// ActionID generator
	actionID atomic.Int64

	// Pending responses: ActionID -> channel waiting for response
	pendingMu sync.Mutex
	pending   map[string]chan *Response

	// Lifecycle
	done     chan struct{}
	closed   atomic.Bool
	readerWg sync.WaitGroup

	// Reconnection
	reconnectMu sync.Mutex
}

// Response holds a parsed AMI response
type Response struct {
	Headers map[string]string
	Success bool
	Message string
}

func NewClient(host string, port int, user, password string, logger *zap.Logger) *Client {
	return &Client{
		host:     host,
		port:     port,
		user:     user,
		password: password,
		logger:   logger,
		pending:  make(map[string]chan *Response),
		done:     make(chan struct{}),
	}
}

// Connect establishes the AMI connection, logs in, and starts the reader loop
func (c *Client) Connect() error {
	var err error
	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	c.conn, err = net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("AMI connect to %s: %w", addr, err)
	}
	c.reader = bufio.NewReader(c.conn)

	banner, err := c.reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("reading AMI banner: %w", err)
	}
	c.logger.Info("AMI connected", zap.String("banner", strings.TrimSpace(banner)))

	// Login is synchronous (reader loop not started yet)
	resp, err := c.sendActionSync(map[string]string{
		"Action":   "Login",
		"Username": c.user,
		"Secret":   c.password,
	})
	if err != nil {
		return fmt.Errorf("AMI login: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("AMI login failed: %s", resp.Message)
	}

	c.logger.Info("AMI logged in successfully")

	// Start async reader goroutine
	c.done = make(chan struct{})
	c.closed.Store(false)
	c.readerWg.Add(1)
	go c.readLoop()

	return nil
}

// readLoop runs in a dedicated goroutine, reading all AMI messages and
// dispatching responses to waiting callers by ActionID.
// Async events (no matching ActionID) are logged and discarded.
func (c *Client) readLoop() {
	defer c.readerWg.Done()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		headers, err := c.readMessage()
		if err != nil {
			if c.closed.Load() {
				return
			}
			// Timeout is expected when idle — just loop and check c.done
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			c.logger.Error("AMI read error in reader loop", zap.Error(err))
			// Fail all pending waiters
			c.failAllPending(err)
			return
		}

		actionID := headers["ActionID"]

		// If there's a pending waiter for this ActionID, deliver it
		if actionID != "" {
			c.pendingMu.Lock()
			ch, exists := c.pending[actionID]
			if exists {
				delete(c.pending, actionID)
			}
			c.pendingMu.Unlock()

			if exists {
				resp := &Response{
					Headers: headers,
					Success: strings.EqualFold(headers["Response"], "Success"),
					Message: headers["Message"],
				}
				// Non-blocking send (channel is buffered with 1)
				select {
				case ch <- resp:
				default:
				}
				continue
			}
		}

		// No pending waiter → async event from Asterisk, log at debug level
		eventType := headers["Event"]
		if eventType != "" {
			c.logger.Debug("AMI async event (ignored)",
				zap.String("event", eventType),
				zap.String("channel", headers["Channel"]))
		}
	}
}

// readMessage reads one complete AMI message (headers until blank line)
func (c *Client) readMessage() (map[string]string, error) {
	headers := make(map[string]string)

	// Short deadline so shutdown/reconnect can interrupt the read quickly.
	// The ping loop keeps the connection alive, so 5s without data is fine.
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading AMI message: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) == 2 {
			headers[parts[0]] = parts[1]
		}
	}

	return headers, nil
}

// failAllPending sends nil to all waiting channels so they unblock with error
func (c *Client) failAllPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

// sendAction sends an AMI action and waits for the response asynchronously.
// Multiple goroutines can call this concurrently — writes are serialized,
// but reads are handled by the reader goroutine.
func (c *Client) sendAction(fields map[string]string) (*Response, error) {
	id := fmt.Sprintf("%d", c.actionID.Add(1))
	fields["ActionID"] = id

	// Create a buffered channel to receive the response
	respCh := make(chan *Response, 1)

	// Register before writing so we don't miss the response
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	// Serialize writes only (not reads)
	c.writeMu.Lock()
	var sb strings.Builder
	for k, v := range fields {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	sb.WriteString("\r\n")

	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := c.conn.Write([]byte(sb.String()))
	c.writeMu.Unlock()

	if err != nil {
		// Clean up pending
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("writing AMI action: %w", err)
	}

	// Wait for response with timeout
	select {
	case resp, ok := <-respCh:
		if !ok || resp == nil {
			return nil, fmt.Errorf("AMI connection lost while waiting for ActionID %s", id)
		}
		return resp, nil
	case <-time.After(10 * time.Second):
		// Clean up pending on timeout
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("AMI response timeout for ActionID %s", id)
	}
}

// sendActionSync is used ONLY during Connect (before readLoop starts).
// It does synchronous write+read like the old implementation.
func (c *Client) sendActionSync(fields map[string]string) (*Response, error) {
	id := fmt.Sprintf("%d", c.actionID.Add(1))
	fields["ActionID"] = id

	var sb strings.Builder
	for k, v := range fields {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	sb.WriteString("\r\n")

	c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.conn.Write([]byte(sb.String())); err != nil {
		return nil, fmt.Errorf("writing AMI action: %w", err)
	}

	// Read response directly (no reader loop yet)
	c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		headers, err := c.readMessage()
		if err != nil {
			return nil, err
		}
		// Skip async events that arrive before our login response
		if headers["ActionID"] == id || headers["Response"] != "" {
			return &Response{
				Headers: headers,
				Success: strings.EqualFold(headers["Response"], "Success"),
				Message: headers["Message"],
			}, nil
		}
	}
}

// Transfer performs a blind transfer of a channel to a destination
func (c *Client) Transfer(channel, destination, context string) error {
	if context == "" {
		context = "from-internal"
	}
	resp, err := c.sendAction(map[string]string{
		"Action":   "Redirect",
		"Channel":  channel,
		"Exten":    destination,
		"Context":  context,
		"Priority": "1",
	})
	if err != nil {
		return fmt.Errorf("AMI transfer: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("AMI transfer failed: %s", resp.Message)
	}
	c.logger.Info("AMI transfer initiated",
		zap.String("channel", channel),
		zap.String("destination", destination))
	return nil
}

// Hangup hangs up a channel
func (c *Client) Hangup(channel string) error {
	resp, err := c.sendAction(map[string]string{
		"Action":  "Hangup",
		"Channel": channel,
		"Cause":   "16",
	})
	if err != nil {
		return fmt.Errorf("AMI hangup: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("AMI hangup failed: %s", resp.Message)
	}
	return nil
}

// Originate creates a new outbound call via SIP or PJSIP
func (c *Client) Originate(destination, callerID, uuid, bridgeAddr string, variables map[string]string) error {
	return c.OriginateWithTrunk(destination, callerID, uuid, bridgeAddr, "", variables)
}

// OriginateWithTrunk creates an outbound call using a specific SIP trunk
func (c *Client) OriginateWithTrunk(destination, callerID, uuid, bridgeAddr, sipTrunk string, variables map[string]string) error {
	vars := make([]string, 0, len(variables))
	for k, v := range variables {
		vars = append(vars, fmt.Sprintf("%s=%s", k, v))
	}

	channel := fmt.Sprintf("SIP/%s", destination)
	if sipTrunk != "" {
		channel = fmt.Sprintf("PJSIP/%s@%s", destination, sipTrunk)
	}

	action := map[string]string{
		"Action":      "Originate",
		"Channel":     channel,
		"Application": "AudioSocket",
		"Data":        fmt.Sprintf("%s,%s", uuid, bridgeAddr),
		"CallerID":    callerID,
		"Timeout":     "30000",
		"Async":       "true",
	}
	if len(vars) > 0 {
		action["Variable"] = strings.Join(vars, ",")
	}

	resp, err := c.sendAction(action)
	if err != nil {
		return fmt.Errorf("AMI originate: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("AMI originate failed: %s", resp.Message)
	}
	c.logger.Info("AMI originate initiated",
		zap.String("channel", channel),
		zap.String("destination", destination),
		zap.String("uuid", uuid))
	return nil
}

// OriginateWithRetry attempts Originate with retries and exponential backoff
func (c *Client) OriginateWithRetry(destination, callerID, uuid, bridgeAddr, sipTrunk string, variables map[string]string, maxRetries int, retryIntervalSec int) error {
	if maxRetries < 1 {
		maxRetries = 1
	}
	if retryIntervalSec < 1 {
		retryIntervalSec = 30
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(retryIntervalSec) * time.Second
			c.logger.Info("Retrying originate",
				zap.Int("attempt", attempt+1),
				zap.Duration("delay", delay))
			time.Sleep(delay)
		}

		lastErr = c.OriginateWithTrunk(destination, callerID, uuid, bridgeAddr, sipTrunk, variables)
		if lastErr == nil {
			return nil
		}
		c.logger.Warn("Originate attempt failed",
			zap.Int("attempt", attempt+1),
			zap.Error(lastErr))
	}

	return fmt.Errorf("originate failed after %d attempts: %w", maxRetries, lastErr)
}

// DialplanReload sends "dialplan reload" to Asterisk via AMI Command action
func (c *Client) DialplanReload() error {
	resp, err := c.sendAction(map[string]string{
		"Action":  "Command",
		"Command": "dialplan reload",
	})
	if err != nil {
		return fmt.Errorf("AMI dialplan reload: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("AMI dialplan reload failed: %s", resp.Message)
	}
	c.logger.Info("AMI dialplan reload completed")
	return nil
}

// GetChannelVar reads a channel variable from Asterisk
func (c *Client) GetChannelVar(channel, variable string) (string, error) {
	resp, err := c.sendAction(map[string]string{
		"Action":   "Getvar",
		"Channel":  channel,
		"Variable": variable,
	})
	if err != nil {
		return "", fmt.Errorf("AMI getvar: %w", err)
	}
	if !resp.Success {
		return "", fmt.Errorf("AMI getvar failed: %s", resp.Message)
	}
	return resp.Headers["Value"], nil
}

// SetVar sets a channel variable
func (c *Client) SetVar(channel, variable, value string) error {
	resp, err := c.sendAction(map[string]string{
		"Action":   "Setvar",
		"Channel":  channel,
		"Variable": variable,
		"Value":    value,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("AMI setvar failed: %s", resp.Message)
	}
	return nil
}

// Ping sends a keep-alive ping to AMI
func (c *Client) Ping() error {
	resp, err := c.sendAction(map[string]string{
		"Action": "Ping",
	})
	if err != nil {
		return fmt.Errorf("AMI ping: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("AMI ping failed: %s", resp.Message)
	}
	return nil
}

// StartPingLoop sends periodic pings to keep the AMI connection alive.
func (c *Client) StartPingLoop(done <-chan struct{}, interval time.Duration) {
	if interval < 10*time.Second {
		interval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := c.Ping(); err != nil {
					c.logger.Warn("AMI ping failed, attempting reconnect", zap.Error(err))
					if reconnErr := c.Reconnect(); reconnErr != nil {
						c.logger.Error("AMI reconnect failed", zap.Error(reconnErr))
					}
				}
			}
		}
	}()
}

// Reconnect re-establishes the AMI connection safely
func (c *Client) Reconnect() error {
	c.reconnectMu.Lock()
	defer c.reconnectMu.Unlock()

	// Stop reader loop
	c.stopReader()

	// Close old connection
	if c.conn != nil {
		c.conn.Close()
	}

	// Reconnect
	return c.Connect()
}

// stopReader signals the reader loop to stop and waits for it
func (c *Client) stopReader() {
	c.closed.Store(true)
	select {
	case <-c.done:
		// already closed
	default:
		close(c.done)
	}
	c.readerWg.Wait()
	c.failAllPending(fmt.Errorf("AMI reconnecting"))
}

// Close disconnects from AMI
func (c *Client) Close() error {
	c.stopReader()

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.conn == nil {
		return nil
	}

	// Best-effort logoff
	id := fmt.Sprintf("%d", c.actionID.Add(1))
	logoff := fmt.Sprintf("Action: Logoff\r\nActionID: %s\r\n\r\n", id)
	c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	c.conn.Write([]byte(logoff))

	return c.conn.Close()
}
