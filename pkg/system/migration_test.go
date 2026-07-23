package system

import "testing"

func TestValidateRegistry(t *testing.T) {
	tests := []struct {
		name     string
		versions []int64
		wantErr  bool
	}{
		{"empty", nil, false},
		{"single", []int64{1}, false},
		{"contiguous", []int64{1, 2, 3}, false},
		{"does not start at 1", []int64{2}, true},
		{"gap", []int64{1, 3}, true},
		{"duplicate", []int64{1, 2, 2}, true},
		{"out of order", []int64{2, 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := make([]SystemMigration, len(tt.versions))
			for i, v := range tt.versions {
				ms[i] = SystemMigration{Version: v}
			}
			if err := validateRegistry(ms); (err != nil) != tt.wantErr {
				t.Fatalf("validateRegistry(%v) error = %v, wantErr %v", tt.versions, err, tt.wantErr)
			}
		})
	}
}

// The real registry must always be valid, so an out-of-order or gapped step
// added later fails the build's tests, not production.
func TestSystemMigrationsRegistryValid(t *testing.T) {
	if err := validateRegistry(systemMigrations); err != nil {
		t.Fatalf("systemMigrations invalid: %v", err)
	}
}
