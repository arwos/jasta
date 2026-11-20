package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const MaxSnapshotBytes = 8 << 20

type response struct {
	Revision uint64 `json:"revision"`
	Error    string `json:"error,omitempty"`
}

type Server struct {
	listener net.Listener
	apply    func(Snapshot) error
}

func Listen(path string, apply func(Snapshot) error) (*Server, error) {
	if path == "" {
		return nil, fmt.Errorf("reload socket path is required")
	}
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on reload socket %q: %w", path, err)
	}
	if err = os.Chmod(path, 0660); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("set reload socket permissions: %w", err)
	}

	s := &Server{listener: listener, apply: apply}
	go s.serve()
	return s, nil
}

func (s *Server) Close() error {
	if s == nil || s.listener == nil {
		return nil
	}
	path := s.listener.Addr().String()
	err := s.listener.Close()
	removeErr := os.Remove(path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(err, removeErr)
}

func (s *Server) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	var request Snapshot
	data, err := io.ReadAll(io.LimitReader(conn, MaxSnapshotBytes+1))
	if err == nil && len(data) > MaxSnapshotBytes {
		err = fmt.Errorf("snapshot exceeds %d bytes", MaxSnapshotBytes)
	}
	if err == nil {
		err = json.Unmarshal(data, &request)
	}
	if err == nil {
		err = s.apply(request)
	}

	result := response{Revision: request.Revision}
	if err != nil {
		result.Error = err.Error()
	}
	_ = json.NewEncoder(conn).Encode(result)
}

func Send(path string, data []byte) (uint64, error) {
	if len(data) == 0 || len(data) > MaxSnapshotBytes {
		return 0, fmt.Errorf("snapshot size must be between 1 and %d bytes", MaxSnapshotBytes)
	}
	conn, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		return 0, fmt.Errorf("connect to web reload socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err = conn.Write(data); err != nil {
		return 0, fmt.Errorf("send snapshot: %w", err)
	}
	if unixConn, ok := conn.(*net.UnixConn); ok {
		if err = unixConn.CloseWrite(); err != nil {
			return 0, fmt.Errorf("finish snapshot: %w", err)
		}
	}

	var result response
	if err = json.NewDecoder(conn).Decode(&result); err != nil {
		return 0, fmt.Errorf("read reload response: %w", err)
	}
	if result.Error != "" {
		return result.Revision, fmt.Errorf("web rejected snapshot: %s", result.Error)
	}
	return result.Revision, nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect reload socket %q: %w", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("reload socket path %q exists and is not a socket", path)
	}
	conn, dialErr := net.DialTimeout("unix", path, time.Second)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("reload socket %q is already active", path)
	}
	if err = os.Remove(path); err != nil {
		return fmt.Errorf("remove stale reload socket %q: %w", path, err)
	}
	return nil
}
