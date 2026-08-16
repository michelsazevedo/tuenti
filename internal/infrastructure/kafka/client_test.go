package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testTopic = "identity.events"
	testKey   = "5f8d0d55-1c2b-4d3e-9a7f-6b1c2d3e4f50"
)

type fakeWriter struct {
	calls    int
	messages []kafkago.Message
	err      error

	closed   int
	closeErr error
}

func (f *fakeWriter) WriteMessages(_ context.Context, msgs ...kafkago.Message) error {
	f.calls++
	f.messages = append(f.messages, msgs...)

	if f.err != nil {
		return f.err
	}

	return nil
}

func (f *fakeWriter) Close() error {
	f.closed++

	return f.closeErr
}

func TestPublishWritesTheEncodedEvent(t *testing.T) {
	writer := &fakeWriter{}

	payload := struct {
		Event string `json:"event"`
		Email string `json:"email"`
	}{Event: "password_reset_requested", Email: "wile.coyote@example.com"}

	require.NoError(t, newProducer(writer).Publish(context.Background(), testTopic, testKey, payload))

	require.Equal(t, 1, writer.calls, "the producer must write exactly once")
	require.Len(t, writer.messages, 1, "the producer must emit exactly one message")

	message := writer.messages[0]
	assert.Equal(t, testTopic, message.Topic, "the message must go to the requested topic")
	assert.Equal(t, []byte(testKey), message.Key, "the partition key must be carried verbatim")

	assert.JSONEq(t,
		`{"event":"password_reset_requested","email":"wile.coyote@example.com"}`,
		string(message.Value),
		"the payload must be JSON encoded",
	)
}

func TestPublishWrapsWriteFailures(t *testing.T) {
	sentinel := errors.New("broker unavailable")
	writer := &fakeWriter{err: sentinel}

	err := newProducer(writer).Publish(context.Background(), testTopic, testKey, map[string]string{"event": "signup"})

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "the underlying client error must stay unwrappable")
	assert.Contains(t, err.Error(), "publishing event to topic")
	assert.Contains(t, err.Error(), testTopic, "the failing topic must be identifiable from the error")
}

func TestPublishWrapsMarshalFailures(t *testing.T) {
	writer := &fakeWriter{}

	err := newProducer(writer).Publish(context.Background(), testTopic, testKey, make(chan int))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshaling event")

	var marshalErr *json.UnsupportedTypeError
	assert.ErrorAs(t, err, &marshalErr, "the encoding error must stay unwrappable")
	assert.Zero(t, writer.calls, "nothing must be written when the payload cannot be encoded")
}

func TestCloseReleasesTheWriter(t *testing.T) {
	writer := &fakeWriter{}

	require.NoError(t, newProducer(writer).Close())
	assert.Equal(t, 1, writer.closed, "the underlying writer must be closed exactly once")
}

func TestCloseWrapsWriterFailures(t *testing.T) {
	sentinel := errors.New("flush timed out")
	writer := &fakeWriter{closeErr: sentinel}

	err := newProducer(writer).Close()

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "the underlying client error must stay unwrappable")
	assert.Contains(t, err.Error(), "closing kafka producer")
}
