package rabbitmq

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/mamed-gasimov/ai-service/internal/messaging"
)

const dlxExchange = "dlx"

var _ messaging.Publisher = (*Client)(nil)
var _ messaging.Consumer = (*Client)(nil)

type Client struct {
	url string

	mu   sync.RWMutex
	conn *amqp.Connection
	ch   *amqp.Channel

	done chan struct{}
}

func NewClient(url string) (*Client, error) {
	c := &Client{
		url:  url,
		done: make(chan struct{}),
	}

	if err := c.connect(); err != nil {
		return nil, err
	}

	go c.reconnectLoop()

	log.Println("RabbitMQ connected, queues and DLQs declared")
	return c, nil
}

func (c *Client) connect() error {
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return fmt.Errorf("amqp dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("amqp open channel: %w", err)
	}

	if err := setupDLX(ch); err != nil {
		ch.Close()
		conn.Close()
		return err
	}

	queues := []string{"file.analyze", "file.analysis.result"}
	for _, q := range queues {
		args := amqp.Table{
			"x-dead-letter-exchange":    dlxExchange,
			"x-dead-letter-routing-key": q + ".dlq",
		}
		if _, err := ch.QueueDeclare(q, true, false, false, false, args); err != nil {
			ch.Close()
			conn.Close()
			return fmt.Errorf("declare queue %q: %w", q, err)
		}
	}

	c.mu.Lock()
	c.conn = conn
	c.ch = ch
	c.mu.Unlock()

	return nil
}

func setupDLX(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(dlxExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare DLX exchange: %w", err)
	}

	dlqs := []string{"file.analyze.dlq", "file.analysis.result.dlq"}
	for _, q := range dlqs {
		if _, err := ch.QueueDeclare(q, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare DLQ %q: %w", q, err)
		}
		if err := ch.QueueBind(q, q, dlxExchange, false, nil); err != nil {
			return fmt.Errorf("bind DLQ %q: %w", q, err)
		}
	}

	return nil
}

func (c *Client) reconnectLoop() {
	for {
		c.mu.RLock()
		connCloseCh := c.conn.NotifyClose(make(chan *amqp.Error, 1))
		c.mu.RUnlock()

		select {
		case <-c.done:
			return
		case amqpErr, ok := <-connCloseCh:
			if !ok {
				return
			}
			log.Printf("RabbitMQ connection lost: %v — reconnecting", amqpErr)
		}

		backoff := time.Second
		for {
			select {
			case <-c.done:
				return
			case <-time.After(backoff):
			}

			if err := c.connect(); err != nil {
				log.Printf("RabbitMQ reconnect failed: %v — retrying in %v", err, backoff)
				if backoff < 30*time.Second {
					backoff *= 2
				}
				continue
			}

			log.Println("RabbitMQ reconnected successfully")
			break
		}
	}
}

func (c *Client) channel() *amqp.Channel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ch
}

func (c *Client) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	return c.channel().PublishWithContext(ctx, exchange, routingKey, false, false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		},
	)
}

func (c *Client) Consume(ctx context.Context, queue string) (<-chan messaging.Delivery, error) {
	deliveries, err := c.channel().Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("amqp consume %q: %w", queue, err)
	}

	out := make(chan messaging.Delivery)
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
				out <- messaging.Delivery{
					Body: d.Body,
					Ack:  func() error { return d.Ack(false) },
					Nack: func() error { return d.Nack(false, false) },
				}
			}
		}
	}()
	return out, nil
}

// Ping returns nil when the underlying AMQP connection is still open.
func (c *Client) Ping() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.conn.IsClosed() {
		return fmt.Errorf("rabbitmq connection is closed")
	}
	return nil
}

func (c *Client) Close() error {
	close(c.done)

	c.mu.RLock()
	ch := c.ch
	conn := c.conn
	c.mu.RUnlock()

	var firstErr error
	if err := ch.Close(); err != nil {
		firstErr = fmt.Errorf("close amqp channel: %w", err)
	}
	if err := conn.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("close amqp connection: %w", err)
	}
	return firstErr
}
