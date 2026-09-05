package vulkan

import (
	"context"

	"github.com/agentstax/vulkan/pkg/metrics"
)

// MeasurementHandle is one measurement series' message key plus the client,
// holding no row.
type MeasurementHandle struct {
	messageKey string
	client     *Client
}

// Measurements returns every current measurement series, ordered by message
// key.
func (s *SystemHandle) Measurements(ctx context.Context) ([]*StoredMessage[Measurement], error) {
	return s.client.admin.ListMeasurements(ctx)
}

// Measurement names a measurement series. No I/O and no failure -- each verb
// on the handle resolves the series' message key when called.
func (s *SystemHandle) Measurement(name string, attributes map[string]string) *MeasurementHandle {
	return &MeasurementHandle{messageKey: metrics.MeasurementKey(name, attributes), client: s.client}
}

func (m *MeasurementHandle) MessageKey() string {
	return m.messageKey
}

// Get returns the measurement series' current value, or nil if no retained
// message has the key.
func (m *MeasurementHandle) Get(ctx context.Context) (*StoredMessage[Measurement], error) {
	return m.client.admin.GetMeasurement(ctx, m.messageKey)
}

// Messages returns the measurement series' retained values, newest first.
func (m *MeasurementHandle) Messages(ctx context.Context, limit int) ([]*StoredMessage[Measurement], error) {
	return m.client.admin.ListMeasurementMessages(ctx, m.messageKey, limit)
}
