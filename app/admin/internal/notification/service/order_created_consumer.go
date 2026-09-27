package service

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/protobuf/proto"

	eventv1 "github.com/eagle-go/eagle/api/eagle/event/v1"
	notificationapp "github.com/eagle-go/eagle/app/admin/internal/notification/application"
	"github.com/eagle-go/eagle/pkg/messaging/rabbitmq"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

const orderCreatedEventType = "eagle.event.v1.OrderCreatedV1"

func NewOrderCreatedConsumer(config *config.Messaging_RabbitMQ, usecase *notificationapp.Usecase, logger *slog.Logger) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		rabbitmq.RunConsumer(ctx, rabbitmq.Config{
			URL: config.GetUrl(), Exchange: config.GetExchange(),
			ReconnectBackoff: config.GetReconnectBackoff().AsDuration(),
			ConsumerAttempts: int(config.GetConsumerMaxAttempts()),
			RetryBackoff:     config.GetConsumerRetryBackoff().AsDuration(),
		}, config.GetOrderCreatedQueue(), "order.created.v1", logger, func(ctx context.Context, message rabbitmq.Message) error {
			if message.Type != orderCreatedEventType {
				return rabbitmq.Permanent(fmt.Errorf("notification: unexpected event type %q", message.Type))
			}
			var envelope eventv1.EventEnvelope
			if err := proto.Unmarshal(message.Body, &envelope); err != nil {
				return rabbitmq.Permanent(fmt.Errorf("notification: decode event envelope: %w", err))
			}
			if envelope.GetEventId() == "" || envelope.GetEventType() != orderCreatedEventType ||
				envelope.GetAggregateType() != "order" || envelope.GetSchemaVersion() != 1 ||
				(message.ID != "" && message.ID != envelope.GetEventId()) {
				return rabbitmq.Permanent(fmt.Errorf("notification: invalid order-created envelope"))
			}

			carrier := propagation.MapCarrier{}
			if envelope.GetTraceparent() != "" {
				carrier.Set("traceparent", envelope.GetTraceparent())
			}
			ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
			ctx, span := otel.Tracer("github.com/eagle-go/eagle/app/admin/notification").Start(ctx, "consume "+orderCreatedEventType)
			defer span.End()

			var event eventv1.OrderCreatedV1
			if err := proto.Unmarshal(envelope.GetPayload(), &event); err != nil {
				return rabbitmq.Permanent(fmt.Errorf("notification: decode order-created event: %w", err))
			}
			if event.GetEventId() != envelope.GetEventId() || event.GetOrderId() != envelope.GetAggregateId() ||
				event.GetOwnerSubject() == "" || event.GetTotalCents() <= 0 {
				return rabbitmq.Permanent(fmt.Errorf("notification: payload does not match envelope"))
			}
			return usecase.ReceiveOrderCreated(
				ctx, event.GetEventId(), event.GetOwnerSubject(), event.GetOrderId(), event.GetTotalCents(),
			)
		})
	}()
	return func() { cancel(); <-done }
}
