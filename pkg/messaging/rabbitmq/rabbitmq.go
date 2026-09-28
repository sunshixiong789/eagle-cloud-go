// Package rabbitmq provides reliable publish confirms and reconnecting consumers.
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	messagingMeter       = otel.Meter("github.com/eagle-go/eagle/pkg/messaging/rabbitmq")
	messagesPublished    = mustCounter("eagle_messaging_published_total", "RabbitMQ messages confirmed by the broker")
	messagePublishFailed = mustCounter("eagle_messaging_publish_failures_total", "RabbitMQ publish attempts that failed")
	messagesConsumed     = mustCounter("eagle_messaging_consumed_total", "RabbitMQ messages handled and acknowledged")
	messageConsumeFailed = mustCounter("eagle_messaging_consume_failures_total", "RabbitMQ handler attempts that failed")
	messagesRetried      = mustCounter("eagle_messaging_retried_total", "RabbitMQ handler retry attempts")
	messagesDeadLettered = mustCounter("eagle_messaging_dead_lettered_total", "RabbitMQ messages rejected to a dead-letter queue")
	consumerDisconnects  = mustCounter("eagle_messaging_consumer_disconnects_total", "RabbitMQ consumer disconnects")
)

type Config struct {
	URL              string
	Exchange         string
	ReconnectBackoff time.Duration
	ConsumerAttempts int
	RetryBackoff     time.Duration
	PublishTimeout   time.Duration
}

type Message struct {
	ID         string
	Type       string
	RoutingKey string
	Body       []byte
	Timestamp  time.Time
	Headers    amqp.Table
}

type Publisher struct {
	config  Config
	gate    chan struct{}
	socket  net.Conn
	returns <-chan amqp.Return
	conn    *amqp.Connection
	ch      *amqp.Channel
}

func NewPublisher(config Config) (*Publisher, error) {
	if config.URL == "" || config.Exchange == "" {
		return nil, errors.New("rabbitmq: URL and exchange are required")
	}
	if config.PublishTimeout <= 0 {
		config.PublishTimeout = 5 * time.Second
	}
	return &Publisher{config: config, gate: make(chan struct{}, 1)}, nil
}

// ErrUnroutable means no queue accepted the routing key; an ack alone is insufficient.
var ErrUnroutable = errors.New("rabbitmq: message was not routed to a queue")

func (p *Publisher) Publish(ctx context.Context, message Message) error {
	ctx, cancel := context.WithTimeout(ctx, p.config.PublishTimeout)
	defer cancel()
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.gate }()
	if err := p.ensureConnected(ctx); err != nil {
		p.recordPublishFailure(ctx, message)
		return err
	}
	deadline, _ := ctx.Deadline()
	if err := p.socket.SetDeadline(deadline); err != nil {
		p.reset()
		return err
	}
	socket := p.socket
	stop := closeOnCancel(ctx, socket)
	defer func() { stop(); _ = socket.SetDeadline(time.Time{}) }()
	confirmation, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, p.config.Exchange, message.RoutingKey, true, false, amqp.Publishing{
		Headers: message.Headers, ContentType: "application/protobuf", DeliveryMode: amqp.Persistent,
		MessageId: message.ID, Type: message.Type, Timestamp: message.Timestamp, Body: message.Body,
	})
	if err != nil {
		p.reset()
		p.recordPublishFailure(ctx, message)
		return fmt.Errorf("rabbitmq: publish: %w", err)
	}
	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		p.reset()
		p.recordPublishFailure(ctx, message)
		return fmt.Errorf("rabbitmq: wait publisher confirm: %w", err)
	}
	if !acked {
		p.reset()
		p.recordPublishFailure(ctx, message)
		return errors.New("rabbitmq: broker rejected published message")
	}
	// AMQP dispatch delivers basic.return before its confirm. With one in-flight
	// publication, the buffered return belongs to this message and is now observable.
	select {
	case returned, ok := <-p.returns:
		p.recordPublishFailure(ctx, message)
		if !ok {
			p.reset()
			return errors.New("rabbitmq: publisher channel closed")
		}
		return fmt.Errorf("%w: %d %s (%s)", ErrUnroutable, returned.ReplyCode, returned.ReplyText, returned.RoutingKey)
	default:
	}
	messagesPublished.Add(ctx, 1, metric.WithAttributes(
		attribute.String("messaging.destination.name", p.config.Exchange),
		attribute.String("messaging.rabbitmq.routing_key", message.RoutingKey),
		attribute.String("messaging.message.type", message.Type),
	))
	return nil
}

func (p *Publisher) recordPublishFailure(ctx context.Context, message Message) {
	messagePublishFailed.Add(ctx, 1, metric.WithAttributes(
		attribute.String("messaging.destination.name", p.config.Exchange),
		attribute.String("messaging.rabbitmq.routing_key", message.RoutingKey),
		attribute.String("messaging.message.type", message.Type),
	))
}

func (p *Publisher) ensureConnected(ctx context.Context) error {
	if p.conn != nil && !p.conn.IsClosed() && p.ch != nil && !p.ch.IsClosed() {
		return nil
	}
	p.reset()
	stop := func() {}
	defer func() { stop() }()
	conn, err := amqp.DialConfig(p.config.URL, amqp.Config{Dial: func(network, address string) (net.Conn, error) {
		socket, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		deadline, _ := ctx.Deadline()
		if err := socket.SetDeadline(deadline); err != nil {
			_ = socket.Close()
			return nil, err
		}
		stop = closeOnCancel(ctx, socket)
		p.socket = socket
		return socket, nil
	}})
	if err != nil {
		p.reset()
		return fmt.Errorf("rabbitmq: connect publisher: %w", err)
	}
	deadline, _ := ctx.Deadline()
	if err := p.socket.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		p.reset()
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("rabbitmq: open publisher channel: %w", err)
	}
	if err := ch.ExchangeDeclare(p.config.Exchange, "topic", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("rabbitmq: declare exchange: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("rabbitmq: enable publisher confirms: %w", err)
	}
	p.returns = ch.NotifyReturn(make(chan amqp.Return, 1))
	p.conn, p.ch = conn, ch
	return nil
}

// Join a cancellation callback before reusing a connection; otherwise a callback
// racing with the next publication could close that publication's transport.
func closeOnCancel(ctx context.Context, socket net.Conn) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = socket.Close(); close(done) })
	return func() {
		if !stop() {
			<-done
		}
	}
}

func (p *Publisher) Close() {
	p.gate <- struct{}{}
	defer func() { <-p.gate }()
	p.reset()
}

func (p *Publisher) reset() {
	// Closing the transport also interrupts a blocked AMQP write or handshake.
	if p.socket != nil {
		_ = p.socket.Close()
	}
	if p.conn != nil {
		_ = p.conn.CloseDeadline(time.Now().Add(time.Second))
	}
	p.conn, p.ch, p.socket, p.returns = nil, nil, nil, nil
}

type Handler func(context.Context, Message) error

type permanentError struct{ error }

// Permanent marks malformed or unsupported messages that retries cannot repair.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{error: err}
}

func IsPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}

// RunConsumer reconnects until ctx is cancelled. Transient handler failures use
// bounded exponential backoff; malformed permanent failures are dead-lettered immediately.
func RunConsumer(ctx context.Context, config Config, queue, routingKey string, logger *slog.Logger, handler Handler) {
	for ctx.Err() == nil {
		err := consume(ctx, config, queue, routingKey, logger, handler)
		if ctx.Err() != nil {
			return
		}
		consumerDisconnects.Add(ctx, 1, metric.WithAttributes(attribute.String("messaging.destination.name", queue)))
		logger.ErrorContext(ctx, "RabbitMQ consumer disconnected", "queue", queue, "error", err)
		timer := time.NewTimer(config.ReconnectBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func consume(ctx context.Context, config Config, queue, routingKey string, logger *slog.Logger, handler Handler) error {
	conn, err := amqp.Dial(config.URL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer func() { _ = ch.Close() }()
	if err := declareConsumerTopology(ch, config.Exchange, queue, routingKey); err != nil {
		return err
	}
	if err := ch.Qos(32, 0, false); err != nil {
		return fmt.Errorf("set qos: %w", err)
	}
	deliveries, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("delivery channel closed")
			}
			message := Message{
				ID: delivery.MessageId, Type: delivery.Type, RoutingKey: delivery.RoutingKey,
				Body: delivery.Body, Timestamp: delivery.Timestamp, Headers: delivery.Headers,
			}
			attrs := metric.WithAttributes(
				attribute.String("messaging.destination.name", queue),
				attribute.String("messaging.rabbitmq.routing_key", delivery.RoutingKey),
				attribute.String("messaging.message.type", delivery.Type),
			)
			handleErr := handleWithRetry(ctx, config.ConsumerAttempts, config.RetryBackoff, message, handler, func() {
				messagesRetried.Add(ctx, 1, attrs)
			})
			if ctx.Err() != nil {
				return ctx.Err()
			} // Closing the channel requeues in-flight work on shutdown.
			if handleErr != nil {
				messageConsumeFailed.Add(ctx, 1, attrs)
				messagesDeadLettered.Add(ctx, 1, attrs)
				logger.ErrorContext(ctx, "RabbitMQ message dead-lettered",
					"queue", queue, "message_id", delivery.MessageId, "error", handleErr,
				)
				if nackErr := delivery.Nack(false, false); nackErr != nil {
					return fmt.Errorf("nack message: %w", errors.Join(handleErr, nackErr))
				}
				continue
			}
			if err := delivery.Ack(false); err != nil {
				return fmt.Errorf("ack message: %w", err)
			}
			messagesConsumed.Add(ctx, 1, attrs)
		}
	}
}

func handleWithRetry(
	ctx context.Context,
	attempts int,
	backoff time.Duration,
	message Message,
	handler Handler,
	onRetry func(),
) error {
	if attempts < 1 {
		attempts = 5
	}
	if backoff <= 0 {
		backoff = time.Second
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		err := handler(ctx, message)
		if err == nil || IsPermanent(err) || attempt == attempts {
			return err
		}
		if onRetry != nil {
			onRetry()
		}
		timer := time.NewTimer(backoff * time.Duration(1<<(attempt-1)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func mustCounter(name, description string) metric.Int64Counter {
	instrument, err := messagingMeter.Int64Counter(name, metric.WithDescription(description))
	if err != nil {
		panic(fmt.Sprintf("rabbitmq: create counter %s: %v", name, err))
	}
	return instrument
}

func declareConsumerTopology(ch *amqp.Channel, exchange, queue, routingKey string) error {
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	dlx := exchange + ".dlx"
	if err := ch.ExchangeDeclare(dlx, "fanout", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead-letter exchange: %w", err)
	}
	dlq := queue + ".dlq"
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, amqp.Table{"x-queue-type": "quorum"}); err != nil {
		return fmt.Errorf("declare dead-letter queue: %w", err)
	}
	if err := ch.QueueBind(dlq, "", dlx, false, nil); err != nil {
		return fmt.Errorf("bind dead-letter queue: %w", err)
	}
	// Retain the source message until the DLQ confirms its own durable write.
	args := amqp.Table{
		"x-queue-type":           "quorum",
		"x-dead-letter-exchange": dlx,
		"x-dead-letter-strategy": "at-least-once",
		"x-overflow":             "reject-publish",
	}
	if _, err := ch.QueueDeclare(queue, true, false, false, false, args); err != nil {
		return fmt.Errorf("declare queue: %w", err)
	}
	if err := ch.QueueBind(queue, routingKey, exchange, false, nil); err != nil {
		return fmt.Errorf("bind queue: %w", err)
	}
	return nil
}
