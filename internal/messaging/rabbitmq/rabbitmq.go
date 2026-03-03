package rabbitmq

import (
	"context"
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/mamed-gasimov/ai-service/internal/messaging"
)

var _ messaging.Publisher = (*Client)(nil)
var _ messaging.Consumer = (*Client)(nil)

type Client struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewClient(url string) (*Client, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("amqp dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("amqp open channel: %w", err)
	}

	c := &Client{conn: conn, ch: ch}

	for _, q := range []string{"file.analyze", "file.analysis.result"} {
		if _, err := ch.QueueDeclare(q, true, false, false, false, nil); err != nil {
			ch.Close()
			conn.Close()
			return nil, fmt.Errorf("declare queue %q: %w", q, err)
		}
	}

	log.Println("RabbitMQ connected, queues declared")
	return c, nil
}

func (c *Client) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	return c.ch.PublishWithContext(ctx, exchange, routingKey, false, false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		},
	)
}

func (c *Client) Consume(ctx context.Context, queue string) (<-chan []byte, error) {
	deliveries, err := c.ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("amqp consume %q: %w", queue, err)
	}

	out := make(chan []byte)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case d, ok := <-deliveries:
				if !ok {
					return
				}
				out <- d.Body
				d.Ack(false)
			}
		}
	}()
	return out, nil
}

func (c *Client) Close() error {
	if err := c.ch.Close(); err != nil {
		return fmt.Errorf("close amqp channel: %w", err)
	}
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("close amqp connection: %w", err)
	}
	return nil
}
