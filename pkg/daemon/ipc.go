package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// Request is a JSON-RPC request.
type Request struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC response.
type Response struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Status holds run status for `syla status`.
type Status struct {
	RunID     string `json:"run_id"`
	RunDir    string `json:"run_dir"`
	Provider  string `json:"provider"`
	Status    string `json:"status"`
	Iteration int    `json:"iteration"`
	Elapsed   string `json:"elapsed"`
	Duration  string `json:"duration"`
	StopReason string `json:"stop_reason,omitempty"`
}

// Server serves IPC over Unix socket.
type Server struct {
	sockPath string
	listener net.Listener
	mu       sync.Mutex
	handlers map[string]func(json.RawMessage) (any, error)
	subs     map[net.Conn]chan []byte
}

func NewServer(sockPath string) *Server {
	return &Server{
		sockPath: sockPath,
		handlers: make(map[string]func(json.RawMessage) (any, error)),
		subs:     make(map[net.Conn]chan []byte),
	}
}

func (s *Server) Handle(method string, fn func(json.RawMessage) (any, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = fn
}

func (s *Server) Listen() error {
	if err := os.MkdirAll(filepath.Dir(s.sockPath), 0755); err != nil {
		return err
	}
	_ = os.Remove(s.sockPath)
	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return err
	}
	s.listener = ln
	go s.acceptLoop()
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	// increase buffer
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		s.mu.Lock()
		fn := s.handlers[req.Method]
		s.mu.Unlock()
		var res Response
		res.ID = req.ID
		if fn == nil {
			res.Error = fmt.Sprintf("unknown method %q", req.Method)
		} else {
			result, err := fn(req.Params)
			if err != nil {
				res.Error = err.Error()
			} else if result != nil {
				b, _ := json.Marshal(result)
				res.Result = b
			}
		}
		b, _ := json.Marshal(res)
		b = append(b, '\n')
		_, _ = conn.Write(b)
		// for Subscribe, keep connection open and stream events
		if req.Method == "Subscribe" {
			ch := make(chan []byte, 64)
			s.mu.Lock()
			s.subs[conn] = ch
			s.mu.Unlock()
			for msg := range ch {
				_, _ = conn.Write(msg)
			}
			return
		}
	}
}

func (s *Server) Broadcast(event any) {
	b, _ := json.Marshal(event)
	b = append(b, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- b:
		default:
		}
	}
}

func (s *Server) Close() error {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	_ = os.Remove(s.sockPath)
	s.mu.Lock()
	for _, ch := range s.subs {
		close(ch)
	}
	s.mu.Unlock()
	return nil
}

func SockPath(runDir string) string {
	return filepath.Join(runDir, "ctl.sock")
}
