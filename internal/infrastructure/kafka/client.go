package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/michelsazevedo/tuenti/internal/infrastructure/config"
)

type kafkaWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafkago.Message) error
	Close() error
}

type Producer struct {
	writer kafkaWriter
}

func NewProducer(conf *config.Config) *Producer {
	return newProducer(&kafkago.Writer{
		Addr:                   kafkago.TCP(conf.GetKafkaBrokers()...),
		Balancer:               &kafkago.LeastBytes{},
		AllowAutoTopicCreation: true,
	})
}

func newProducer(writer kafkaWriter) *Producer {
	return &Producer{writer: writer}
}

func (p *Producer) Publish(ctx context.Context, topic, key string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling event: %w", err)
	}

	if err := p.writer.WriteMessages(ctx, kafkago.Message{Topic: topic, Key: []byte(key), Value: body}); err != nil {
		return fmt.Errorf("publishing event to topic %q: %w", topic, err)
	}

	return nil
}

// Close releases the connections the underlying writer keeps open to the
// brokers. It is called once, from the fx OnStop hook.
func (p *Producer) Close() error {
	if err := p.writer.Close(); err != nil {
		return fmt.Errorf("closing kafka producer: %w", err)
	}

	return nil
}
