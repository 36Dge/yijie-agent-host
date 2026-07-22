package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
)

const methodNotFoundCode = -32601

var errClientClosed = errors.New("app-server client closed")

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("app-server error %d: %s", e.Code, e.Message)
}

type wireMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type pendingResponse struct {
	result json.RawMessage
	err    error
}

type outboundMessage struct {
	payload []byte
	done    chan error
}

type Client struct {
	stdin           io.WriteCloser
	stdout          io.Reader
	maxMessageBytes int
	writes          chan outboundMessage
	onNotification  func(string, json.RawMessage)
	onFatal         func(error)

	mu      sync.Mutex
	nextID  int64
	pending map[string]chan pendingResponse
	closed  chan struct{}
	fatal   error

	startOnce sync.Once
	failOnce  sync.Once
	inputOnce sync.Once
}

func NewClient(
	stdin io.WriteCloser,
	stdout io.Reader,
	maxMessageBytes int,
	writeQueueDepth int,
	onNotification func(string, json.RawMessage),
	onFatal func(error),
) *Client {
	return &Client{
		stdin:           stdin,
		stdout:          stdout,
		maxMessageBytes: maxMessageBytes,
		writes:          make(chan outboundMessage, writeQueueDepth),
		onNotification:  onNotification,
		onFatal:         onFatal,
		nextID:          -1,
		pending:         make(map[string]chan pendingResponse),
		closed:          make(chan struct{}),
	}
}

func (c *Client) Start() {
	c.startOnce.Do(func() {
		go c.writeLoop()
		go c.readLoop()
	})
}

func (c *Client) Request(ctx context.Context, method string, params any, result any) error {
	if method == "" {
		return errors.New("app-server request method is required")
	}

	c.mu.Lock()
	select {
	case <-c.closed:
		err := c.fatal
		c.mu.Unlock()
		if err != nil {
			return err
		}
		return errClientClosed
	default:
	}
	c.nextID++
	id := c.nextID
	key := numericIDKey(id)
	response := make(chan pendingResponse, 1)
	c.pending[key] = response
	c.mu.Unlock()

	payload, err := json.Marshal(struct {
		Method string `json:"method"`
		ID     int64  `json:"id"`
		Params any    `json:"params"`
	}{Method: method, ID: id, Params: params})
	if err != nil {
		c.removePending(key)
		return fmt.Errorf("encode app-server request: %w", err)
	}
	if err := c.enqueue(ctx, append(payload, '\n')); err != nil {
		c.removePending(key)
		return err
	}

	select {
	case received := <-response:
		if received.err != nil {
			return received.err
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(received.result, result); err != nil {
			return fmt.Errorf("decode app-server response for %s: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		c.removePending(key)
		return ctx.Err()
	case <-c.closed:
		c.removePending(key)
		c.mu.Lock()
		fatal := c.fatal
		c.mu.Unlock()
		if fatal != nil {
			return fatal
		}
		return errClientClosed
	}
}

func (c *Client) Notify(ctx context.Context, method string, params any) error {
	if method == "" {
		return errors.New("app-server notification method is required")
	}
	payload, err := json.Marshal(struct {
		Method string `json:"method"`
		Params any    `json:"params"`
	}{Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("encode app-server notification: %w", err)
	}
	return c.enqueue(ctx, append(payload, '\n'))
}

func (c *Client) CloseInput() error {
	var err error
	c.inputOnce.Do(func() {
		err = c.stdin.Close()
	})
	return err
}

func (c *Client) enqueue(ctx context.Context, payload []byte) error {
	done := make(chan error, 1)
	select {
	case c.writes <- outboundMessage{payload: payload, done: done}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errClientClosed
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errClientClosed
	}
}

func (c *Client) writeLoop() {
	for {
		select {
		case message := <-c.writes:
			if _, err := c.stdin.Write(message.payload); err != nil {
				if message.done != nil {
					message.done <- err
				}
				c.fail(fmt.Errorf("write app-server message: %w", err))
				return
			}
			if message.done != nil {
				message.done <- nil
			}
		case <-c.closed:
			return
		}
	}
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	initialBuffer := 64 * 1024
	if c.maxMessageBytes < initialBuffer {
		initialBuffer = c.maxMessageBytes
	}
	scanner.Buffer(make([]byte, initialBuffer), c.maxMessageBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var message wireMessage
		if err := json.Unmarshal(line, &message); err != nil {
			c.fail(fmt.Errorf("decode app-server message: %w", err))
			return
		}
		if err := c.handleMessage(message); err != nil {
			c.fail(err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		c.fail(fmt.Errorf("read app-server message: %w", err))
		return
	}
	c.fail(io.EOF)
}

func (c *Client) handleMessage(message wireMessage) error {
	hasID := len(message.ID) > 0
	if message.Method != "" {
		if !hasID {
			if c.onNotification != nil {
				c.onNotification(message.Method, message.Params)
			}
			return nil
		}
		if _, err := requestIDKey(message.ID); err != nil {
			return err
		}
		return c.rejectServerRequest(message.ID)
	}
	if !hasID {
		return errors.New("invalid app-server message without method or id")
	}

	key, err := requestIDKey(message.ID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	response, ok := c.pending[key]
	if ok {
		delete(c.pending, key)
	}
	c.mu.Unlock()
	if !ok {
		return nil
	}
	if message.Error != nil {
		response <- pendingResponse{err: message.Error}
		return nil
	}
	if len(message.Result) == 0 {
		response <- pendingResponse{err: errors.New("app-server response has neither result nor error")}
		return nil
	}
	response <- pendingResponse{result: message.Result}
	return nil
}

func (c *Client) rejectServerRequest(id json.RawMessage) error {
	payload, err := json.Marshal(struct {
		ID    json.RawMessage `json:"id"`
		Error RPCError        `json:"error"`
	}{
		ID: id,
		Error: RPCError{
			Code:    methodNotFoundCode,
			Message: "Method not supported by Yijie Agent Host Runtime Baseline 2",
		},
	})
	if err != nil {
		return fmt.Errorf("encode app-server reverse-request rejection: %w", err)
	}
	select {
	case c.writes <- outboundMessage{payload: append(payload, '\n')}:
		return nil
	default:
		return errors.New("app-server write queue is full")
	}
}

func (c *Client) removePending(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
}

func (c *Client) fail(err error) {
	c.failOnce.Do(func() {
		c.mu.Lock()
		c.fatal = err
		close(c.closed)
		c.mu.Unlock()
		if c.onFatal != nil {
			c.onFatal(err)
		}
	})
}

func requestIDKey(raw json.RawMessage) (string, error) {
	var numeric int64
	if err := json.Unmarshal(raw, &numeric); err == nil {
		return numericIDKey(numeric), nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return "s:" + text, nil
	}
	return "", errors.New("app-server request id must be an int64 or string")
}

func numericIDKey(id int64) string {
	return "n:" + strconv.FormatInt(id, 10)
}
