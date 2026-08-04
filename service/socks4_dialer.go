package service

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type socks4Dialer struct {
	proxyAddress string
	userId       string
	timeout      time.Duration
}

func newSOCKS4Dialer(proxyURL *url.URL, timeout time.Duration) (*socks4Dialer, error) {
	if proxyURL == nil || strings.TrimSpace(proxyURL.Host) == "" {
		return nil, errors.New("SOCKS4 proxy address is required")
	}
	if _, _, err := net.SplitHostPort(proxyURL.Host); err != nil {
		return nil, errors.New("SOCKS4 proxy address must include a valid port")
	}
	userId := ""
	if proxyURL.User != nil {
		userId = proxyURL.User.Username()
		if _, hasPassword := proxyURL.User.Password(); hasPassword {
			return nil, errors.New("SOCKS4 supports a user ID but not password authentication")
		}
	}
	if strings.ContainsRune(userId, '\x00') || len(userId) > 255 {
		return nil, errors.New("SOCKS4 user ID is invalid")
	}
	return &socks4Dialer{proxyAddress: proxyURL.Host, userId: userId, timeout: timeout}, nil
}

func (dialer *socks4Dialer) DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	if dialer == nil {
		return nil, errors.New("SOCKS4 dialer is nil")
	}
	if !strings.HasPrefix(strings.ToLower(network), "tcp") {
		return nil, fmt.Errorf("SOCKS4 only supports TCP, got %s", network)
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("SOCKS4 destination must include a valid port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("SOCKS4 destination port is invalid")
	}
	destinationIP, err := resolveSOCKS4IPv4(ctx, host)
	if err != nil {
		return nil, err
	}

	netDialer := &net.Dialer{Timeout: dialer.timeout}
	conn, err := netDialer.DialContext(ctx, "tcp", dialer.proxyAddress)
	if err != nil {
		return nil, errors.New("SOCKS4 proxy connection failed")
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = conn.Close()
		}
	}()
	handshakeDone := make(chan struct{})
	defer close(handshakeDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-handshakeDone:
		}
	}()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else if dialer.timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(dialer.timeout))
	}

	request := make([]byte, 0, 9+len(dialer.userId))
	request = append(request, 0x04, 0x01, 0x00, 0x00)
	binary.BigEndian.PutUint16(request[2:4], uint16(port))
	request = append(request, destinationIP...)
	request = append(request, []byte(dialer.userId)...)
	request = append(request, 0x00)
	if _, err := conn.Write(request); err != nil {
		return nil, errors.New("SOCKS4 handshake write failed")
	}
	response := make([]byte, 8)
	if _, err := io.ReadFull(conn, response); err != nil {
		return nil, errors.New("SOCKS4 handshake response failed")
	}
	if response[0] != 0x00 && response[0] != 0x04 {
		return nil, errors.New("SOCKS4 proxy returned an invalid response")
	}
	if response[1] != 0x5a {
		return nil, socks4ResponseError(response[1])
	}
	_ = conn.SetDeadline(time.Time{})
	succeeded = true
	return conn, nil
}

func resolveSOCKS4IPv4(ctx context.Context, host string) (net.IP, error) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if parsed := net.ParseIP(host); parsed != nil {
		if ipv4 := parsed.To4(); ipv4 != nil {
			return ipv4, nil
		}
		return nil, errors.New("SOCKS4 does not support IPv6 destinations")
	}
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("SOCKS4 destination DNS lookup failed")
	}
	for _, address := range addresses {
		if ipv4 := address.To4(); ipv4 != nil {
			return ipv4, nil
		}
	}
	return nil, errors.New("SOCKS4 destination has no IPv4 address")
}

func socks4ResponseError(code byte) error {
	switch code {
	case 0x5b:
		return errors.New("SOCKS4 request rejected or failed")
	case 0x5c:
		return errors.New("SOCKS4 identity service is unavailable")
	case 0x5d:
		return errors.New("SOCKS4 identity verification failed")
	default:
		return fmt.Errorf("SOCKS4 request failed with code 0x%02x", code)
	}
}
