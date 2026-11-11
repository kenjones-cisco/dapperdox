package main

import "testing"

func TestManagementLoopbackOnly(t *testing.T) {
	for _, address := range []string{"0.0.0.0:0", "[::]:0", "192.0.2.1:0", "localhost:0", ":0", "invalid"} {
		if listener, err := listenManagement(address); err == nil {
			_ = listener.Close()

			t.Errorf("accepted unsafe management address %q", address)
		}
	}

	listener, err := listenManagement("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}
