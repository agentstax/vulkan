package admin

import "context"

// RegisterSystem stands up the shared control-plane schema every topic rides
// on. Call it once before registering any topic.
//
// Idempotent and config-free -- safe to call on every service start, a no-op
// once the schema is present.
func (a *MessageAdmin) RegisterSystem(ctx context.Context) error {
	return a.systemDatastore.RegisterSystem(ctx)
}
