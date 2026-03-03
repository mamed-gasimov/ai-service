package messaging

import "context"

type Publisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
	Close() error
}

type Consumer interface {
	Consume(ctx context.Context, queue string) (<-chan []byte, error)
	Close() error
}
