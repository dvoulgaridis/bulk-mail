package mail

import "context"

type SenderIdentity struct {
	Email   string
	Name    string
	ReplyTo string
}

type SendResult struct {
	ProviderMessageID string
}

type Sender interface {
	Send(context.Context, Message) (SendResult, error)
}

type CloseSender interface {
	Sender
	Close() error
}

type ConnectionTester interface {
	TestConnection(context.Context) error
}
