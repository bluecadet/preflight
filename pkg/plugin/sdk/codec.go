package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// errHandleUnavailable is returned by noopHandleServer; defined here to keep the
// protocol file free of imports beyond context.
var errHandleUnavailable = errors.New("plugin handle: no target is bound")

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// frame is the unified JSON-RPC 2.0 envelope used by the codec. A frame is
// a request (Method + ID), a response (ID + Result/Error, no Method), or a
// notification (Method, no ID). ID is carried as raw JSON so incoming request
// IDs are echoed back byte-for-byte in the response.
type frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// requestHandler dispatches an incoming request (host→plugin or plugin→host)
// and returns either a JSON-marshalable result or a JSON-RPC error. It runs in
// its own goroutine so the read loop never blocks on a handler — handlers may
// themselves issue outgoing calls back through the same codec (the handle-op
// round trip: a module's Check, while handling the host's "check" request,
// calls RunCommand, which sends a request the host must answer while the
// "check" call is still outstanding).
type requestHandler func(ctx context.Context, method string, params json.RawMessage) (result any, rpcErr *rpcError)

// notificationHandler dispatches an incoming notification (no ID, no response).
type notificationHandler func(params json.RawMessage)

// codec is the shared bidirectional newline-JSON-RPC framing used by both the
// host Client and the plugin server. It owns:
//   - a single read-loop goroutine that routes every incoming frame:
//     responses to pending calls (matched by request ID), notifications to the
//     notification handler, and requests to the request handler (in a goroutine
//     so handling can issue its own calls without stalling the read loop);
//   - a mutex-guarded writer so outgoing frames (requests, responses,
//     notifications) are never interleaved;
//   - a call mutex that serializes outgoing calls to one in flight at a time
//     (the protocol's stated one-in-flight-op limitation), with a pending-map
//     channel per call for response correlation.
type codec struct {
	r   io.Reader
	w   io.Writer
	enc *json.Encoder
	dec *json.Decoder

	writeMu sync.Mutex

	callMu    sync.Mutex
	pendingMu sync.Mutex
	pending   map[int64]chan *frame
	seq       atomic.Int64

	handlerWG sync.WaitGroup

	reqHandler   requestHandler
	notifHandler notificationHandler

	ctx    context.Context
	cancel context.CancelFunc

	closeOnce sync.Once
	closed    atomic.Bool
	closeErr  error
	closeFn   func() error
}

// newCodec constructs a codec over r/w. reqHandler handles incoming requests;
// notifHandler handles incoming notifications. Call start to launch the read
// loop. closeFn (if non-nil) releases the underlying transport on Close.
func newCodec(r io.Reader, w io.Writer, reqHandler requestHandler, notifHandler notificationHandler, closeFn func() error) *codec {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	ctx, cancel := context.WithCancel(context.Background())
	return &codec{
		r:            r,
		w:            w,
		enc:          json.NewEncoder(w),
		dec:          dec,
		pending:      make(map[int64]chan *frame),
		reqHandler:   reqHandler,
		notifHandler: notifHandler,
		ctx:          ctx,
		cancel:       cancel,
		closeFn:      closeFn,
	}
}

// start launches the read loop. It returns immediately.
func (c *codec) start() {
	go c.readLoop()
}

func (c *codec) readLoop() {
	for {
		var f frame
		if err := c.dec.Decode(&f); err != nil {
			c.fail(err)
			return
		}
		switch {
		case f.Method != "" && len(f.ID) > 0:
			// Incoming request. Handle in a goroutine so the read loop keeps
			// draining; the handler may issue outgoing calls (handle ops).
			id, ok := parseID(f.ID)
			if !ok {
				c.replyRaw(f.ID, nil, &rpcError{Code: -32600, Message: "invalid request id"})
				continue
			}
			c.handlerWG.Go(func() {
				c.serveRequest(id, f.ID, f.Method, f.Params)
			})
		case f.Method != "":
			// Notification.
			if c.notifHandler != nil {
				c.notifHandler(f.Params)
			}
		case len(f.ID) > 0:
			// Response to one of our calls.
			id, ok := parseID(f.ID)
			if !ok {
				continue
			}
			ch := c.takePending(id)
			if ch != nil {
				ch <- &f
			}
		}
	}
}

// serveRequest runs the request handler for an incoming request and writes the
// response. The read loop increments c.handlerWG before launching this so
// Wait cannot observe a zero counter before a handler is tracked.
func (c *codec) serveRequest(_ int64, rawID json.RawMessage, method string, params json.RawMessage) {
	result, rpcErr := c.reqHandler(c.ctx, method, params)
	c.replyRaw(rawID, result, rpcErr)
}

// call sends a request, waits for the correlated response, and decodes the
// result into out (which may be nil to discard). One call is in flight at a
// time per codec (callMu held for the round trip) — the protocol's stated
// one-in-flight-op limitation.
func (c *codec) call(ctx context.Context, method string, params any, out any) error {
	c.callMu.Lock()
	defer c.callMu.Unlock()

	id := c.seq.Add(1)
	idRaw, err := json.Marshal(id)
	if err != nil {
		return err
	}

	f := frame{
		JSONRPC: "2.0",
		ID:      idRaw,
		Method:  method,
	}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("encode params: %w", err)
		}
		f.Params = raw
	}

	ch := make(chan *frame, 1)
	c.putPending(id, ch)
	defer c.takePending(id)

	if err := c.writeFrame(f); err != nil {
		return err
	}

	select {
	case resp := <-ch:
		if resp == nil {
			// fail() closed the channel (peer gone); ctx.Done may not have
			// been observed first.
			return errors.New("plugin connection closed")
		}
		if resp.Error != nil {
			return fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		if out != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("unmarshal result: %w", err)
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return errors.New("plugin connection closed")
	}
}

// notify sends a notification (no ID, no response expected).
func (c *codec) notify(method string, params any) error {
	f := frame{
		JSONRPC: "2.0",
		Method:  method,
	}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		f.Params = raw
	}
	return c.writeFrame(f)
}

// replyRaw writes a response for the given (echoed) request ID.
func (c *codec) replyRaw(rawID json.RawMessage, result any, rpcErr *rpcError) {
	f := frame{JSONRPC: "2.0", ID: rawID}
	if rpcErr != nil {
		f.Error = rpcErr
	} else if result != nil {
		raw, err := json.Marshal(result)
		if err != nil {
			f.Error = &rpcError{Code: -32603, Message: "internal error: " + err.Error()}
		} else {
			f.Result = raw
		}
	}
	_ = c.writeFrame(f)
}

func (c *codec) writeFrame(f frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.enc.Encode(f)
}

func (c *codec) putPending(id int64, ch chan *frame) {
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()
}

func (c *codec) takePending(id int64) chan *frame {
	c.pendingMu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.pendingMu.Unlock()
	return ch
}

// fail is called when the read loop ends (EOF or decode error). All pending
// calls are woken with a closed-connection error.
func (c *codec) fail(cause error) {
	c.cancel()
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
	c.pendingMu.Unlock()
}

// Close shuts down the codec: cancels the context (failing pending calls
// and ending the read loop), waits for in-flight request handlers, and runs
// the transport close function once. The read loop exits when the transport
// close produces EOF on the reader.
func (c *codec) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		c.cancel()
		c.handlerWG.Wait()
		if c.closeFn != nil {
			c.closeErr = c.closeFn()
		}
	})
	return c.closeErr
}

// parseID decodes a JSON-RPC id raw value into an int64. Both sides generate
// int64 sequence IDs, so ids are always JSON numbers.
func parseID(raw json.RawMessage) (int64, bool) {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	i, err := n.Int64()
	if err != nil {
		return 0, false
	}
	return i, true
}
