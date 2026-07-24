package migrate

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		versions []int64
		wantErr  bool
	}{
		{"empty", nil, false},
		{"single", []int64{2}, false},
		{"contiguous", []int64{2, 3, 4}, false},
		{"does not start at 2", []int64{1}, true},
		{"does not start at 2 (higher)", []int64{3}, true},
		{"gap", []int64{2, 4}, true},
		{"duplicate", []int64{2, 3, 3}, true},
		{"out of order", []int64{3, 2}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := make([]Migration, len(tt.versions))
			for i, v := range tt.versions {
				registry[i] = Migration{Version: v}
			}
			if err := Validate(registry); (err != nil) != tt.wantErr {
				t.Fatalf("Validate(%v) error = %v, wantErr %v", tt.versions, err, tt.wantErr)
			}
		})
	}
}
