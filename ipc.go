package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
)

const ipcQueueSize = 256

var errIPCClosed = errors.New("ipc: connection closed")

type ipcMessage struct {
	ID     uint64            `json:"id,omitempty"`
	Method string            `json:"method,omitempty"`
	Params []json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage   `json:"result,omitempty"`
	Error  string            `json:"error,omitempty"`
	Event  string            `json:"event,omitempty"`
	Data   json.RawMessage   `json:"data,omitempty"`
}

func (m ipcMessage) isResponse() bool {
	return m.ID != 0 && m.Method == "" && m.Event == ""
}

type ipcConn struct {
	dec    *json.Decoder
	enc    *json.Encoder
	closer io.Closer
	out    chan ipcMessage
	done   chan struct{}

	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan ipcMessage
	closed  bool
}

func newIPCConn(r io.Reader, w io.WriteCloser) *ipcConn {
	c := &ipcConn{
		dec:     json.NewDecoder(r),
		enc:     json.NewEncoder(w),
		closer:  w,
		out:     make(chan ipcMessage, ipcQueueSize),
		done:    make(chan struct{}),
		pending: make(map[uint64]chan ipcMessage),
	}
	go c.writeLoop()
	return c
}

func (c *ipcConn) writeLoop() {
	defer c.closer.Close()
	for {
		select {
		case msg := <-c.out:
			if c.enc.Encode(msg) != nil {
				c.close()
				return
			}
		case <-c.done:
			for {
				select {
				case msg := <-c.out:
					if c.enc.Encode(msg) != nil {
						return
					}
				default:
					return
				}
			}
		}
	}
}

func (c *ipcConn) serve(handle func(ipcMessage)) {
	defer c.close()
	for {
		var msg ipcMessage
		if err := c.dec.Decode(&msg); err != nil {
			return
		}
		if msg.isResponse() {
			c.mu.Lock()
			reply, ok := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.mu.Unlock()
			if ok {
				reply <- msg
			}
			continue
		}
		handle(msg)
	}
}

func (c *ipcConn) send(msg ipcMessage) error {
	select {
	case <-c.done:
		return errIPCClosed
	default:
	}
	select {
	case c.out <- msg:
		return nil
	case <-c.done:
		return errIPCClosed
	}
}

func (c *ipcConn) notify(event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.out <- ipcMessage{Event: event, Data: raw}:
	default:
	}
}

func (c *ipcConn) call(method string, result any, params ...any) error {
	raw := make([]json.RawMessage, len(params))
	for i, param := range params {
		encoded, err := json.Marshal(param)
		if err != nil {
			return err
		}
		raw[i] = encoded
	}

	reply := make(chan ipcMessage, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errIPCClosed
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = reply
	c.mu.Unlock()

	if err := c.send(ipcMessage{ID: id, Method: method, Params: raw}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}

	msg, ok := <-reply
	if !ok {
		return errIPCClosed
	}
	if msg.Error != "" {
		return errors.New(msg.Error)
	}
	if result == nil || len(msg.Result) == 0 {
		return nil
	}
	return json.Unmarshal(msg.Result, result)
}

func (c *ipcConn) reply(id uint64, result json.RawMessage, err error) {
	msg := ipcMessage{ID: id, Result: result}
	if err != nil {
		msg.Error = err.Error()
	}
	_ = c.send(msg)
}

func (c *ipcConn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
	for id, reply := range c.pending {
		close(reply)
		delete(c.pending, id)
	}
}

func invokeMethod(target any, name string, params []json.RawMessage) (json.RawMessage, error) {
	method := reflect.ValueOf(target).MethodByName(name)
	if !method.IsValid() {
		return nil, fmt.Errorf("ipc: unknown method %q", name)
	}

	kind := method.Type()
	if kind.NumIn() != len(params) {
		return nil, fmt.Errorf("ipc: %s expects %d arguments, got %d", name, kind.NumIn(), len(params))
	}

	args := make([]reflect.Value, len(params))
	for i, raw := range params {
		arg := reflect.New(kind.In(i))
		if err := json.Unmarshal(raw, arg.Interface()); err != nil {
			return nil, fmt.Errorf("ipc: %s argument %d: %w", name, i+1, err)
		}
		args[i] = arg.Elem()
	}

	out := method.Call(args)
	if len(out) == 0 {
		return nil, nil
	}
	return json.Marshal(out[0].Interface())
}
