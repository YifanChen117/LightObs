package capture

import "testing"

func TestNextPow2(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{1023, 1024},
		{1024, 1024},
		{1025, 2048},
	}

	for _, tt := range tests {
		got := nextPow2(tt.input)
		if got != tt.want {
			t.Errorf("nextPow2(%d) = %d; want %d", tt.input, got, tt.want)
		}
	}
}
