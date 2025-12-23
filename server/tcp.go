package server

import (
	"bufio"
	"fmt"
	"net"
	"strings"

	"github.com/subhammurarka/LedgerKV/db"
)

type Server struct {
	db *db.DB
}

func New(db *db.DB) *Server {
	return &Server{db: db}
}

func (s *Server) Listen(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Println("LedgerKV listening on", addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		resp := s.handleCommand(strings.TrimSpace(line))
		writer.WriteString(resp + "\n")
		writer.Flush()
	}
}

func (s *Server) handleCommand(cmd string) string {
	parts := strings.SplitN(cmd, " ", 3)
	if len(parts) < 2 {
		return "ERROR invalid command"
	}

	switch strings.ToUpper(parts[0]) {

	case "PUT":
		if len(parts) != 3 {
			return "ERROR PUT key value"
		}
		if err := s.db.Put([]byte(parts[1]), []byte(parts[2])); err != nil {
			return "ERROR " + err.Error()
		}
		return "OK"

	case "GET":
		val, ok, err := s.db.Get([]byte(parts[1]))
		if err != nil {
			return "ERROR " + err.Error()
		}
		if !ok {
			return "NOT_FOUND"
		}
		return "VALUE " + string(val)

	case "DEL":
		if err := s.db.Delete([]byte(parts[1])); err != nil {
			return "ERROR " + err.Error()
		}
		return "OK"

	default:
		return "ERROR unknown command"
	}
}
