package server

import (
	"fmt"
	"net/http"
)

type Sender interface {
	Send(string)
}

type Server struct {
	opts Options
}

type Options struct {
	port   int
	mux    *http.ServeMux
	sender Sender
}

const (
	defaultPort = 8080
)

func New() *Server {
	opts := Options{
		port: defaultPort,
	}

	return &Server{
		opts: opts,
	}
}

func (s *Server) Start(opts ...func(*Options)) {
	for _, o := range opts {
		o(&s.opts)
	}

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

func WithMux(mux *http.ServeMux) func(*Options) {
	return func(o *Options) {
		o.mux = mux
	}
}

func WithPort(port int) func(*Options) {
	return func(o *Options) {
		o.port = port
	}
}

func WithSender(sender Sender) func(*Options) {
	return func(o *Options) {
		o.sender = sender
	}
}
