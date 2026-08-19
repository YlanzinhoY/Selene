package achievementserver

import "testing"

func TestValidateLoopbackAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:48212", "[::1]:48212"} {
		if err := validateLoopbackAddress(address); err != nil {
			t.Fatalf("validateLoopbackAddress(%q): %v", address, err)
		}
	}
}

func TestValidateLoopbackAddressRejectsExternalBind(t *testing.T) {
	for _, address := range []string{"0.0.0.0:48212", "192.168.1.20:48212", "localhost:48212", "missing-port"} {
		if err := validateLoopbackAddress(address); err == nil {
			t.Fatalf("validateLoopbackAddress(%q) unexpectedly succeeded", address)
		}
	}
}

func TestInternalEnvironmentSelectsServerMode(t *testing.T) {
	entries := InternalEnvironment("127.0.0.1:50000")
	if len(entries) != 2 || entries[0] != InternalModeEnv+"="+internalModeValue || entries[1] != HTTPAddressEnv+"=127.0.0.1:50000" {
		t.Fatalf("InternalEnvironment() = %#v", entries)
	}
}
