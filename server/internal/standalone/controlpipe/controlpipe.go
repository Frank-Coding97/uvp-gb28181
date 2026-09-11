// Package controlpipe provides an authenticated local control channel.
package controlpipe

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	pipeNamePrefix    = `\\.\pipe\uvp-standalone-control-v1-`
	maxPipeNameLength = 64
	maxSecretLength   = 4096
	controlNonceSize  = 32
	frameHeaderSize   = 4
	maxHandshakeFrame = uint32(4 * 1024)
	handshakeTimeout  = 5 * time.Second
	frameHello        = byte(1)
	frameChallenge    = byte(2)
	frameClientProof  = byte(3)
	frameServerProof  = byte(4)
	controlPipeDomain = "uvp-standalone-controlpipe/v1"
	clientProofLabel  = "client-proof"
	serverProofLabel  = "server-proof"
)

var (
	ErrUnsupported    = errors.New("standalone control pipe unsupported")
	ErrInvalidName    = errors.New("standalone control pipe invalid name")
	ErrInvalidSecret  = errors.New("standalone control pipe invalid secret")
	ErrInvalidContext = errors.New("standalone control pipe invalid context")
	ErrAuthentication = errors.New("standalone control pipe authentication failed")
	ErrMalformedFrame = errors.New("standalone control pipe malformed frame")
	ErrFrameTooLarge  = errors.New("standalone control pipe frame too large")
)

// Listen creates an authenticated local control-pipe listener. name is a
// logical instance name; it is never used as a filesystem path directly.
func Listen(name, secret string) (net.Listener, error) {
	path, err := pipePath(name)
	if err != nil {
		return nil, err
	}
	if err := validateSecret(secret); err != nil {
		return nil, err
	}
	return listenPlatform(path, []byte(secret))
}

// Dial connects to an authenticated local control pipe. The context covers
// both the named-pipe connect and the authentication handshake.
func Dial(ctx context.Context, name, secret string) (net.Conn, error) {
	path, err := pipePath(name)
	if err != nil {
		return nil, err
	}
	if err := validateSecret(secret); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, ErrInvalidContext
	}
	return dialPlatform(ctx, path, []byte(secret))
}

func pipePath(name string) (string, error) {
	if name == "" || len(name) > maxPipeNameLength || strings.TrimSpace(name) != name || strings.TrimRight(name, " .") != name {
		return "", ErrInvalidName
	}
	for index, r := range name {
		if index == 0 && !isASCIIAlphaNumeric(r) {
			return "", ErrInvalidName
		}
		if !isASCIIAlphaNumeric(r) && r != '.' && r != '_' && r != '-' {
			return "", ErrInvalidName
		}
	}
	if name == "." || name == ".." || isReservedDeviceName(name) {
		return "", ErrInvalidName
	}
	return pipeNamePrefix + name, nil
}

func validateSecret(secret string) error {
	if secret == "" || len(secret) > maxSecretLength {
		return ErrInvalidSecret
	}
	return nil
}

func isASCIIAlphaNumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func isReservedDeviceName(value string) bool {
	value = strings.TrimRight(value, " .")
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		value = value[:dot]
	}
	value = strings.ToUpper(value)
	switch value {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9", "COM¹", "COM²", "COM³", "LPT¹", "LPT²", "LPT³":
		return true
	default:
		return false
	}
}

func protectedPipeSDDL(userSID string) string {
	return "D:P(A;;GA;;;" + userSID + ")(A;;GA;;;SY)"
}

func authenticateClient(ctx context.Context, conn net.Conn, secret []byte) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := conn.SetDeadline(handshakeDeadline(ctx)); err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() { result <- clientHandshake(conn, secret) }()
	select {
	case err := <-result:
		if ctxErr := ctx.Err(); ctxErr != nil {
			_ = conn.Close()
			return ctxErr
		}
		if err != nil {
			_ = conn.Close()
		}
		return err
	case <-ctx.Done():
		_ = conn.Close()
		<-result
		return ctx.Err()
	}
}

func authenticateServer(conn net.Conn, secret []byte) error {
	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return err
	}
	return serverHandshake(conn, secret)
}

func handshakeDeadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(handshakeTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}

func clientHandshake(conn net.Conn, secret []byte) error {
	var clientNonce [controlNonceSize]byte
	if _, err := io.ReadFull(rand.Reader, clientNonce[:]); err != nil {
		return fmt.Errorf("generate control handshake nonce: %w", err)
	}
	if err := writeFrame(conn, append([]byte{frameHello}, clientNonce[:]...)); err != nil {
		return err
	}

	challenge, err := readFrame(conn)
	if err != nil {
		return err
	}
	if len(challenge) != 1+controlNonceSize || challenge[0] != frameChallenge {
		return ErrMalformedFrame
	}
	var serverNonce [controlNonceSize]byte
	copy(serverNonce[:], challenge[1:])
	clientProof := makeProof(secret, clientNonce[:], serverNonce[:], clientProofLabel)
	if err := writeFrame(conn, append([]byte{frameClientProof}, clientProof...)); err != nil {
		return err
	}

	serverProof, err := readFrame(conn)
	if err != nil {
		return fmt.Errorf("%w: read server proof: %w", ErrAuthentication, err)
	}
	if len(serverProof) != 1+sha256.Size || serverProof[0] != frameServerProof {
		return ErrAuthentication
	}
	expected := makeProof(secret, clientNonce[:], serverNonce[:], serverProofLabel)
	if !hmac.Equal(serverProof[1:], expected) {
		return ErrAuthentication
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	return nil
}

func serverHandshake(conn net.Conn, secret []byte) error {
	hello, err := readFrame(conn)
	if err != nil {
		return err
	}
	if len(hello) != 1+controlNonceSize || hello[0] != frameHello {
		return ErrMalformedFrame
	}
	var clientNonce [controlNonceSize]byte
	copy(clientNonce[:], hello[1:])
	var serverNonce [controlNonceSize]byte
	if _, err := io.ReadFull(rand.Reader, serverNonce[:]); err != nil {
		return fmt.Errorf("generate control handshake nonce: %w", err)
	}
	if err := writeFrame(conn, append([]byte{frameChallenge}, serverNonce[:]...)); err != nil {
		return err
	}

	clientProof, err := readFrame(conn)
	if err != nil {
		return fmt.Errorf("%w: read client proof: %w", ErrAuthentication, err)
	}
	if len(clientProof) != 1+sha256.Size || clientProof[0] != frameClientProof {
		return ErrAuthentication
	}
	expected := makeProof(secret, clientNonce[:], serverNonce[:], clientProofLabel)
	if !hmac.Equal(clientProof[1:], expected) {
		return ErrAuthentication
	}
	serverProof := makeProof(secret, clientNonce[:], serverNonce[:], serverProofLabel)
	if err := writeFrame(conn, append([]byte{frameServerProof}, serverProof...)); err != nil {
		return err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	return nil
}

func makeProof(secret, clientNonce, serverNonce []byte, label string) []byte {
	digest := hmac.New(sha256.New, secret)
	_, _ = digest.Write([]byte(controlPipeDomain))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(clientNonce)
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(serverNonce)
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(label))
	return digest.Sum(nil)
}

func writeFrame(conn net.Conn, payload []byte) error {
	if len(payload) == 0 {
		return ErrMalformedFrame
	}
	if len(payload) > int(maxHandshakeFrame) {
		return ErrFrameTooLarge
	}
	header := make([]byte, frameHeaderSize)
	binary.BigEndian.PutUint32(header, uint32(len(payload)))
	if err := writeAll(conn, header); err != nil {
		return err
	}
	return writeAll(conn, payload)
}

func readFrame(conn net.Conn) ([]byte, error) {
	header := make([]byte, frameHeaderSize)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedFrame, err)
	}
	length := binary.BigEndian.Uint32(header)
	if length == 0 {
		return nil, ErrMalformedFrame
	}
	if length > maxHandshakeFrame {
		return nil, ErrFrameTooLarge
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedFrame, err)
	}
	return payload, nil
}

func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
