package service

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type socks4HandshakeObservation struct {
	Command     byte
	Destination net.IP
	Port        int
	UserId      string
	RequestHost string
}

func TestNewProxyHTTPClientSupportsSOCKS4(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	observation := make(chan socks4HandshakeObservation, 1)
	serverError := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverError <- acceptErr
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		header := make([]byte, 8)
		if _, readErr := io.ReadFull(reader, header); readErr != nil {
			serverError <- readErr
			return
		}
		userId, readErr := reader.ReadString(0)
		if readErr != nil {
			serverError <- readErr
			return
		}
		if _, writeErr := conn.Write([]byte{0x00, 0x5a, header[2], header[3], header[4], header[5], header[6], header[7]}); writeErr != nil {
			serverError <- writeErr
			return
		}
		request, readErr := http.ReadRequest(reader)
		if readErr != nil {
			serverError <- readErr
			return
		}
		observation <- socks4HandshakeObservation{
			Command: header[1], Destination: net.IPv4(header[4], header[5], header[6], header[7]),
			Port: int(binary.BigEndian.Uint16(header[2:4])), UserId: userId[:len(userId)-1], RequestHost: request.Host,
		}
		_, writeErr := conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"))
		serverError <- writeErr
	}()

	ResetProxyClientCache()
	t.Cleanup(ResetProxyClientCache)
	client, err := NewProxyHttpClient("socks4://tester@" + listener.Addr().String())
	require.NoError(t, err)
	response, err := client.Get("http://127.0.0.1:18080/health")
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, "ok", string(body))

	select {
	case observed := <-observation:
		require.Equal(t, byte(0x01), observed.Command)
		require.True(t, observed.Destination.Equal(net.ParseIP("127.0.0.1")))
		require.Equal(t, 18080, observed.Port)
		require.Equal(t, "tester", observed.UserId)
		require.Equal(t, "127.0.0.1:18080", observed.RequestHost)
	case <-time.After(time.Second):
		t.Fatal("SOCKS4 server did not receive the handshake")
	}
	require.NoError(t, <-serverError)
}

func TestNewProxyHTTPClientRejectsSOCKS4Password(t *testing.T) {
	ResetProxyClientCache()
	_, err := NewProxyHttpClient("socks4://user:password@127.0.0.1:1080")
	require.ErrorContains(t, err, "not password authentication")
}

func TestSOCKS4DialerReportsProxyRejection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		header := make([]byte, 8)
		_, _ = io.ReadFull(reader, header)
		_, _ = reader.ReadString(0)
		_, _ = conn.Write([]byte{0x00, 0x5b, 0, 0, 0, 0, 0, 0})
	}()
	dialer, err := newSOCKS4Dialer(mustParseSOCKS4URL(t, "socks4://"+listener.Addr().String()), time.Second)
	require.NoError(t, err)
	_, err = dialer.DialContext(t.Context(), "tcp", "127.0.0.1:80")
	require.ErrorContains(t, err, "rejected")
}

func TestSOCKS4DialerRespectsContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	handshakeReceived := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		header := make([]byte, 8)
		_, _ = io.ReadFull(reader, header)
		_, _ = reader.ReadString(0)
		close(handshakeReceived)
		_, _ = io.Copy(io.Discard, reader)
	}()
	dialer, err := newSOCKS4Dialer(mustParseSOCKS4URL(t, "socks4://"+listener.Addr().String()), 5*time.Second)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, dialErr := dialer.DialContext(ctx, "tcp", "127.0.0.1:80")
		result <- dialErr
	}()
	select {
	case <-handshakeReceived:
	case <-time.After(time.Second):
		t.Fatal("SOCKS4 handshake was not received")
	}
	cancel()
	select {
	case dialErr := <-result:
		require.Error(t, dialErr)
	case <-time.After(time.Second):
		t.Fatal("SOCKS4 dial did not stop after context cancellation")
	}
}

func TestResolveSOCKS4IPv4RejectsIPv6(t *testing.T) {
	_, err := resolveSOCKS4IPv4(context.Background(), "2001:db8::1")
	require.ErrorContains(t, err, "does not support IPv6")
}

func mustParseSOCKS4URL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	require.NoError(t, err)
	return parsed
}
