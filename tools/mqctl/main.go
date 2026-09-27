// Command mqctl inspects and deliberately redrives a RabbitMQ dead-letter queue.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	command := flag.String("command", "inspect", "inspect or redrive")
	url := flag.String("url", os.Getenv("EAGLE_MESSAGING_RABBITMQ_URL"), "RabbitMQ URL")
	exchange := flag.String("exchange", "eagle.events", "target event exchange")
	queue := flag.String("queue", "eagle.admin.order-created.v1.dlq", "dead-letter queue")
	routingKey := flag.String("routing-key", "", "target routing key; required for redrive")
	limit := flag.Int("limit", 20, "maximum messages to inspect or redrive")
	yes := flag.Bool("yes", false, "confirm redrive")
	flag.Parse()
	if *url == "" || *limit < 1 || *limit > 1000 {
		panic("mqctl: RabbitMQ URL and -limit in [1,1000] are required")
	}
	if *command == "redrive" && (!*yes || *routingKey == "") {
		panic("mqctl: redrive requires -routing-key and -yes")
	}
	conn, err := amqp.Dial(*url)
	if err != nil {
		panic(err)
	}
	defer func() { _ = conn.Close() }()
	channel, err := conn.Channel()
	if err != nil {
		panic(err)
	}
	defer func() { _ = channel.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	switch *command {
	case "inspect":
		err = inspect(channel, *queue, *limit)
	case "redrive":
		err = redrive(ctx, channel, *exchange, *queue, *routingKey, *limit)
	default:
		err = fmt.Errorf("unsupported command %q", *command)
	}
	if err != nil {
		panic(err)
	}
}

func inspect(channel *amqp.Channel, queue string, limit int) error {
	encoder := json.NewEncoder(os.Stdout)
	deliveries := make([]amqp.Delivery, 0, limit)
	defer func() {
		for _, delivery := range deliveries {
			_ = delivery.Nack(false, true)
		}
	}()
	for range limit {
		delivery, ok, err := channel.Get(queue, false)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		deliveries = append(deliveries, delivery)
		value := struct {
			ID           string    `json:"id"`
			Type         string    `json:"type"`
			RoutingKey   string    `json:"routing_key"`
			Timestamp    time.Time `json:"timestamp"`
			BodyBytes    int       `json:"body_bytes"`
			Remaining    uint32    `json:"remaining"`
			WasRedeliver bool      `json:"was_redelivered"`
		}{delivery.MessageId, delivery.Type, delivery.RoutingKey, delivery.Timestamp, len(delivery.Body), delivery.MessageCount, delivery.Redelivered}
		if err := encoder.Encode(value); err != nil {
			return err
		}
	}
	return nil
}

func redrive(ctx context.Context, channel *amqp.Channel, exchange, queue, routingKey string, limit int) error {
	if err := channel.Confirm(false); err != nil {
		return err
	}
	redriven := 0
	for range limit {
		delivery, ok, err := channel.Get(queue, false)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		headers := delivery.Headers
		if headers == nil {
			headers = amqp.Table{}
		}
		headers["x-eagle-redriven-at"] = time.Now().UTC().Format(time.RFC3339)
		confirmation, err := channel.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
			Headers: headers, ContentType: delivery.ContentType, DeliveryMode: amqp.Persistent,
			MessageId: delivery.MessageId, Type: delivery.Type, Timestamp: delivery.Timestamp, Body: delivery.Body,
		})
		if err != nil {
			_ = delivery.Nack(false, true)
			return err
		}
		confirmed, err := confirmation.WaitContext(ctx)
		if err != nil || !confirmed {
			_ = delivery.Nack(false, true)
			if err != nil {
				return err
			}
			return fmt.Errorf("broker rejected redriven message %q", delivery.MessageId)
		}
		if err := delivery.Ack(false); err != nil {
			return err
		}
		redriven++
	}
	fmt.Printf("redriven %d messages\n", redriven)
	return nil
}
