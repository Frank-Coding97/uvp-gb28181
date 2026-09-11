// Package control defines the bounded command protocol carried by controlpipe.
package control

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Command string
type Reply string

const (
	Probe      Command = "probe"
	Stop       Command = "stop"
	Quiesce    Command = "quiesce"
	Finalize   Command = "finalize"
	Ready      Reply   = "ready"
	Stopping   Reply   = "stopping"
	MediaReady Reply   = "media-ready"
	Finalized  Reply   = "finalized"
	Failed     Reply   = "failed"
)

// Endpoint binds both the pipe name and authentication key to an installation
// and a role. A launcher proof cannot be relayed to a backend pipe. The caller
// must first validate the installation paths and protected configuration.
func Endpoint(installDir, role, secret string) (name, key string, err error) {
	if !filepath.IsAbs(installDir) || (role != "launcher" && role != "backend") || secret == "" {
		return "", "", errors.New("invalid control endpoint")
	}
	root := filepath.Clean(installDir)
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	sum := sha256.Sum256([]byte("uvp-control-endpoint-v1\x00" + role + "\x00" + root))
	name = hex.EncodeToString(sum[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("uvp-control-key-v1\x00" + name))
	return name, hex.EncodeToString(mac.Sum(nil)), nil
}

func Exchange(ctx context.Context, conn net.Conn, command Command) (Reply, error) {
	if !validCommand(command) {
		return "", errors.New("invalid control command")
	}
	stop, err := boundConnection(ctx, conn)
	if err != nil {
		return "", err
	}
	defer stop()
	if _, err = io.WriteString(conn, string(command)+"\n"); err != nil {
		return "", err
	}
	line, err := readLine(conn)
	if err != nil {
		return "", err
	}
	reply := Reply(line)
	if !validReply(reply) {
		return "", errors.New("invalid control reply")
	}
	return reply, nil
}

// Handle executes exactly one authenticated command. It neither authenticates
// the transport nor closes it; the owner must do both. Internal handler errors
// become a constant reply, never raw configuration or diagnostics on the wire.
func Handle(ctx context.Context, conn net.Conn, handler func(context.Context, Command) (Reply, error)) error {
	stop, err := boundConnection(ctx, conn)
	if err != nil {
		return err
	}
	defer stop()
	line, err := readLine(conn)
	if err != nil {
		return err
	}
	command := Command(line)
	if !validCommand(command) {
		return errors.New("invalid control command")
	}
	reply, handleErr := handler(ctx, command)
	if handleErr != nil {
		reply = Failed
	}
	if !validReply(reply) {
		return errors.New("invalid control handler reply")
	}
	_, writeErr := io.WriteString(conn, string(reply)+"\n")
	return errors.Join(handleErr, writeErr)
}

func boundConnection(ctx context.Context, conn net.Conn) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(60 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	return func() { stop() }, nil
}

func readLine(conn net.Conn) (string, error) {
	// ReadSlice never grows the buffer. There are no command payloads.
	raw, err := bufio.NewReaderSize(conn, 64).ReadSlice('\n')
	if err != nil {
		return "", errors.New("invalid or incomplete control frame")
	}
	return strings.TrimSuffix(string(raw), "\n"), nil
}
func validCommand(c Command) bool { return c == Probe || c == Stop || c == Quiesce || c == Finalize }
func validReply(r Reply) bool {
	return r == Ready || r == Stopping || r == MediaReady || r == Finalized || r == Failed
}
