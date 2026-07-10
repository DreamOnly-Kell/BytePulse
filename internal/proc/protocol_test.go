package proc

import "testing"

func TestProtocolName(t *testing.T) {
	tests := []struct {
		socketType uint32
		status     string
		want       string
	}{
		{1, "ESTABLISHED", "tcp"},
		{2, "", "udp"},
		{0, "LISTEN", "tcp"},
		{0, "", "udp"},
	}
	for _, tt := range tests {
		if got := protocolName(tt.socketType, tt.status); got != tt.want {
			t.Fatalf("protocolName(%d, %q)=%q, want %q", tt.socketType, tt.status, got, tt.want)
		}
	}
}
