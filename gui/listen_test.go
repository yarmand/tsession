package main

import (
	"net"
	"testing"
)

func TestListenWithFallbackUsesPreferredPortWhenFree(t *testing.T) {
	l, err := listenWithFallback("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listenWithFallback: %v", err)
	}
	defer l.Close()
	if l.Addr().(*net.TCPAddr).Port == 0 {
		t.Fatal("expected a concrete bound port, got 0")
	}
}

func TestListenWithFallbackFallsBackWhenPreferredIsTaken(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port for the test: %v", err)
	}
	defer blocker.Close()
	taken := blocker.Addr().String()

	l, err := listenWithFallback(taken)
	if err != nil {
		t.Fatalf("listenWithFallback: %v", err)
	}
	defer l.Close()

	got := l.Addr().(*net.TCPAddr).Port
	want := blocker.Addr().(*net.TCPAddr).Port
	if got == want {
		t.Fatalf("expected a different port than the taken one %d, got the same", want)
	}
}

func TestListenWithFallbackDoesNotFallbackForNonAddrInUseErrors(t *testing.T) {
	_, err := listenWithFallback("127.0.0.1:-1")
	if err == nil {
		t.Fatal("expected invalid preferred port to return an error")
	}
}
