package message

type Message struct {
	queue chan string
}

func New() *Message {
	return &Message{
		queue: make(chan string, 1),
	}
}

func (m *Message) Send(str string) {
	m.queue <- str
}

func (m *Message) Queue() chan string {
	return m.queue
}
