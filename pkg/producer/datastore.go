package producer

import "context"

// specifically converting WorkType to Message here to be more in line with community standards

type Datastore[Message any] interface {
	AppendMessage(ctx context.Context, message *Message) error
}
