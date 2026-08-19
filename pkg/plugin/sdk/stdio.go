package sdk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// errCodeNotInitialized is returned for any method other than "initialize"
// received before initialize has completed successfully — including a
// second "initialize" once one has already succeeded. It is distinct from
// -32601 (method not found): the method may be perfectly valid, just
// arriving out of the one order the protocol allows.
const errCodeNotInitialized = -32011

// serveIO reads newline-delimited JSON-RPC from r and writes responses to w.
// It is the internal entry point used by Serve and tests. Both sides act as
// client and server: a plugin's Check/Apply may call back to the host for
// handle ops (RunCommand/PutFile/GetFile) over the same channel.
func serveIO(m Module, r io.Reader, w io.Writer, opts ServeOptions) {
	srv := newServer(m, r, w, nil, opts)
	srv.run()
}

// server is the plugin-side JSON-RPC endpoint.
//
// initialize must complete before any other method is dispatched: the codec
// hands each decoded request to its own goroutine as soon as it is decoded
// (see codec.readLoop), so nothing but this gate stops a "check" or "apply"
// from running concurrently with the "initialize" handler that populates
// info and negotiated. ready is closed exactly once, after initialize has
// finished writing info and negotiated; every other handler receives from
// ready before touching them. Per the Go memory model, a channel close
// happens before a receive that observes it returns, so that receive is the
// only synchronization info and negotiated need — no mutex required to read
// them afterward. initMu serializes the initialize handler itself, so a
// second (or concurrent) "initialize" cannot write those fields a second
// time or double-close ready.
type server struct {
	mod          Module
	codec        *codec
	capabilities []string // this plugin's advertised capabilities, from ServeOptions

	initMu      sync.Mutex
	initialized bool
	ready       chan struct{} // closed once initialize completes successfully

	info       TargetInfo // set once, by initialize, before ready is closed
	negotiated Negotiated // set once, by initialize, before ready is closed
}

func newServer(m Module, r io.Reader, w io.Writer, closeFn func() error, opts ServeOptions) *server {
	s := &server{mod: m, capabilities: opts.Capabilities, ready: make(chan struct{})}
	s.codec = newCodec(r, w, s.handleRequest, nil, closeFn, 0)
	return s
}

func (s *server) run() {
	s.codec.start()
	// Block until the read loop ends (peer closed or transport error), then
	// wait for any in-flight request handlers so their responses are written.
	<-s.codec.ctx.Done()
	s.codec.handlerWG.Wait()
}

// handleRequest dispatches an incoming host→plugin request. ctx is scoped to
// this request: the codec cancels it when the host sends a cancel
// notification for it, or when the host goes away. It is handed straight to
// the module's Check/Apply so plugin authors can honour cancellation.
//
// Every method but "initialize" is gated on s.ready: a request arriving
// before initialize has completed gets a clean errCodeNotInitialized error
// instead of being dispatched against not-yet-written (or concurrently being
// written) info/negotiated. This rejection is immediate, not a wait — a
// well-behaved peer never triggers it, since it always receives the
// initialize response before issuing anything else.
func (s *server) handleRequest(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	if method == "initialize" {
		return s.handleInitialize(params)
	}

	select {
	case <-s.ready:
	default:
		return nil, &rpcError{Code: errCodeNotInitialized, Message: fmt.Sprintf("method %q called before initialize completed", method)}
	}

	switch method {
	case "check":
		h := &serverHandle{info: s.info, codec: s.codec}
		args, perr := argsFromParams(params)
		if perr != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid params: " + perr.Error()}
		}
		res, err := s.mod.Check(contextWithNegotiated(ctx, s.negotiated), args, h)
		if err != nil {
			res.Error = err.Error()
		}
		return res, nil

	case "apply":
		h := &serverHandle{info: s.info, codec: s.codec}
		args, perr := argsFromParams(params)
		if perr != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid params: " + perr.Error()}
		}
		res, err := s.mod.Apply(contextWithNegotiated(ctx, s.negotiated), args, h)
		if err != nil {
			res.Error = err.Error()
		}
		return res, nil

	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

// handleInitialize processes the one "initialize" call this session accepts.
// initMu serializes it end to end, so a duplicate or concurrent second call
// cannot race the first's writes to info/negotiated or double-close ready;
// it is simply rejected. On success, info and negotiated are fully written
// before ready is closed, which is what lets every later handler read them
// without a mutex.
func (s *server) handleInitialize(params json.RawMessage) (any, *rpcError) {
	s.initMu.Lock()
	defer s.initMu.Unlock()

	if s.initialized {
		return nil, &rpcError{Code: errCodeNotInitialized, Message: "initialize called more than once"}
	}

	var p initializeParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "initialize params: " + err.Error()}
		}
	}
	local := localProtocolRange(s.capabilities)
	negotiated, err := negotiateProtocol(local, p.protocolRange)
	if err != nil {
		// Symmetric with the host: negotiateProtocol is the same function
		// over the same two ranges, so this fires exactly when the host's
		// own negotiation would. Fail loudly here too, rather than silently
		// answering and relying solely on the host to notice.
		return nil, &rpcError{Code: -32010, Message: err.Error()}
	}

	s.info = p.Target
	s.negotiated = negotiated
	s.initialized = true
	close(s.ready)

	return initializeResult{
		protocolRange: local,
		Name:          s.mod.Name(),
		Version:       s.mod.Version(),
	}, nil
}

// serverHandle is the Handle given to a plugin's Check/Apply. Info returns the
// TargetInfo delivered at initialize; Output emits an output notification;
// RunCommand/PutFile/GetFile send requests to the host over the same codec.
type serverHandle struct {
	info  TargetInfo
	codec *codec
}

func (h *serverHandle) RunCommand(ctx context.Context, script string) (CommandResult, error) {
	var res CommandResult
	if err := h.codec.call(ctx, "run_command", map[string]any{"script": script}, &res); err != nil {
		return CommandResult{}, err
	}
	return res, nil
}

func (h *serverHandle) PutFile(ctx context.Context, path string, data []byte) error {
	return h.codec.call(ctx, "put_file", map[string]any{
		"path": path,
		"data": encodeBase64(data),
	}, nil)
}

func (h *serverHandle) GetFile(ctx context.Context, path string) ([]byte, error) {
	var res getFileResult
	if err := h.codec.call(ctx, "get_file", map[string]any{"path": path}, &res); err != nil {
		return nil, err
	}
	return decodeBase64(res.Data)
}

func (h *serverHandle) Info() TargetInfo { return h.info }

func (h *serverHandle) Output(line string) {
	_ = h.codec.notify("output", map[string]any{"line": line})
}

// argsFromParams extracts the "args" key from JSON-RPC params. Empty params
// and a nil "args" key both legitimately yield an empty map; a malformed JSON
// body surfaces as an error rather than silently running the plugin with no
// args.
func argsFromParams(params json.RawMessage) (map[string]any, error) {
	if len(params) == 0 {
		return map[string]any{}, nil
	}
	var p struct {
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	if p.Args == nil {
		return map[string]any{}, nil
	}
	return p.Args, nil
}

// encodeBase64 / decodeBase64 carry file bytes over JSON-RPC. In v1, files are
// sent as a single base64-encoded frame fully buffered in memory; chunked
// streaming for large files is deferred to v2. Keep PutFile/GetFile payloads
// small (a few MB at most).
func encodeBase64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

func decodeBase64(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(s)
}
