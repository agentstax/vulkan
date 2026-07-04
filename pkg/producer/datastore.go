package producer

import "context"

// specifically converting WorkType to Message here to be more in line with community standards

type Datastore[Message any] interface {
	// routingKey is optional; "" is stored as SQL NULL (no routing key set).
	AppendMessage(ctx context.Context, producerFunc ProducerFunc[Message], routingKey string) (*Message, error)
}
