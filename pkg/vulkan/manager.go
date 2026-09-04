package vulkan

import "context"

// ManagerHandle is a handle on the client's system manager.
type ManagerHandle struct {
	client *Client
}

// Manager returns the client system manager's handle. No I/O and no failure.
func (c *Client) Manager() *ManagerHandle {
	return &ManagerHandle{client: c}
}

// Run claims the system's manager row and reconciles every worker row in the
// deployment until ctx cancels, then returns nil. Safe to run N-way -- the row
// admits one reconcile loop at a time.
func (m *ManagerHandle) Run(ctx context.Context) error {
	return m.client.manager.Run(ctx)
}
