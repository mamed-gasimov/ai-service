package messaging

import "context"

// Delivery wraps a consumed message, allowing the worker to
// acknowledge or negatively-acknowledge (reject → DLQ) each message.
type Delivery struct {
	Body []byte
	Ack  func() error
	Nack func() error
}

type Publisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
	Close() error
}

type Consumer interface {
	Consume(ctx context.Context, queue string) (<-chan Delivery, error)
	Close() error
}
