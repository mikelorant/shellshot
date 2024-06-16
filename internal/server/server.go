package server

import (
	"fmt"
	"net/http"
)

type Server struct {
	opts Options
}

const (
	defaultPort = 8080
)

func New(options ...func(*Options)) *Server {
	opts := Options{
		port: defaultPort,
	}

	for _, o := range options {
		o(&opts)
	}

	return &Server{
		opts: opts,
	}
}

func (s *Server) Start() {
	go func() {
		err := s.Serve()
		if err != nil && s.opts.sender != nil {
			s.opts.sender.Send(fmt.Errorf("unable to start server: %w", err).Error())
		}
	}()
}

func (s *Server) Serve() error {
	addr := fmt.Sprintf(":%v", s.opts.port)

	if s.opts.sender != nil {
		s.opts.sender.Send(fmt.Sprintf("Starting debug server on port: %v", s.opts.port))
	}

	return http.ListenAndServe(addr, s.opts.mux)
}
