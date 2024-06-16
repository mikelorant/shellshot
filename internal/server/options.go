package server

import (
	"net/http"
)

type Sender interface {
	Send(string)
}

type Options struct {
	port   int
	mux    *http.ServeMux
	sender Sender
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
