package proc

import "testing"

func TestProcessKeyIncludesPIDAndCreateTime(t *testing.T) {
	got := processKey(123, 456)
	if got != "123:456" {
		t.Fatalf("processKey=%q, want 123:456", got)
	}
}

func TestDedupeKeyTreatsIdenticalConnectionsAsDuplicates(t *testing.T) {
	c := Connection{
		PID:        123,
		Protocol:   "tcp",
		LocalAddr:  "127.0.0.1",
		LocalPort:  8080,
		RemoteAddr: "127.0.0.1",
		RemotePort: 443,
		Status:     "ESTABLISHED",
	}
	if dedupeKey(c) != dedupeKey(c) {
		t.Fatalf("identical connections produced different keys")
	}
}

func TestUnknownProcessFallbackPreservesPID(t *testing.T) {
	name, path, key := processIdentity(42, "", 0)
	if name != "unknown" {
		t.Fatalf("name=%q, want unknown", name)
	}
	if path != "" {
		t.Fatalf("path=%q, want empty", path)
	}
	if key != "42:0" {
		t.Fatalf("key=%q, want 42:0", key)
	}
}

func TestProcessIdentitySplitsNameFromPath(t *testing.T) {
	name, path, key := processIdentity(42, "/usr/bin/curl", 1000)
	if name != "curl" {
		t.Fatalf("name=%q, want curl", name)
	}
	if path != "/usr/bin/curl" {
		t.Fatalf("path=%q, want /usr/bin/curl", path)
	}
	if key != "42:1000" {
		t.Fatalf("key=%q, want 42:1000", key)
	}
}
