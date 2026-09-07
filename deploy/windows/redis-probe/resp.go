package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	maxRESPLine       = 64 * 1024
	maxRESPBulkLength = 64 * 1024 * 1024
	maxRESPArrayItems = 1_000_000
)

var errRedis = errors.New("redis server error")

type respKind byte

const (
	respSimple  respKind = '+'
	respError   respKind = '-'
	respInteger respKind = ':'
	respBulk    respKind = '$'
	respArray   respKind = '*'
	respNil     respKind = 'n'
)

type respValue struct {
	kind    respKind
	data    []byte
	integer int64
	items   []respValue
	errText string
}

func encodeCommand(args ...string) []byte {
	var buffer bytes.Buffer
	fmt.Fprintf(&buffer, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&buffer, "$%d\r\n", len(arg))
		buffer.WriteString(arg)
		buffer.WriteString("\r\n")
	}
	return buffer.Bytes()
}

func readRESP(reader *bufio.Reader) (respValue, error) {
	return readRESPDepth(reader, 0)
}

func readRESPDepth(reader *bufio.Reader, depth int) (respValue, error) {
	if depth > 128 {
		return respValue{}, errors.New("RESP nesting exceeds limit")
	}
	prefix, err := reader.ReadByte()
	if err != nil {
		return respValue{}, err
	}
	line, err := readRESPLine(reader)
	if err != nil {
		return respValue{}, err
	}
	switch respKind(prefix) {
	case respSimple:
		return respValue{kind: respSimple, data: []byte(line)}, nil
	case respError:
		return respValue{kind: respError, errText: line}, nil
	case respInteger:
		integer, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return respValue{}, fmt.Errorf("invalid RESP integer %q: %w", line, err)
		}
		return respValue{kind: respInteger, integer: integer}, nil
	case respBulk:
		length, err := parseRESPLength(line, maxRESPBulkLength)
		if err != nil {
			return respValue{}, fmt.Errorf("invalid RESP bulk length: %w", err)
		}
		if length == -1 {
			return respValue{kind: respNil}, nil
		}
		data := make([]byte, length+2)
		if _, err := io.ReadFull(reader, data); err != nil {
			return respValue{}, fmt.Errorf("read RESP bulk payload: %w", err)
		}
		if data[length] != '\r' || data[length+1] != '\n' {
			return respValue{}, errors.New("RESP bulk payload is missing CRLF")
		}
		return respValue{kind: respBulk, data: data[:length]}, nil
	case respArray:
		length, err := parseRESPLength(line, maxRESPArrayItems)
		if err != nil {
			return respValue{}, fmt.Errorf("invalid RESP array length: %w", err)
		}
		if length == -1 {
			return respValue{kind: respNil}, nil
		}
		items := make([]respValue, 0, length)
		for index := 0; index < length; index++ {
			item, err := readRESPDepth(reader, depth+1)
			if err != nil {
				return respValue{}, err
			}
			items = append(items, item)
		}
		return respValue{kind: respArray, items: items}, nil
	default:
		return respValue{}, fmt.Errorf("unsupported RESP type %q", prefix)
	}
}

func readRESPLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) < 2 || line[len(line)-2:] != "\r\n" {
		return "", errors.New("RESP line is missing CRLF")
	}
	line = line[:len(line)-2]
	if len(line) > maxRESPLine {
		return "", errors.New("RESP line exceeds limit")
	}
	return line, nil
}

func parseRESPLength(line string, limit int) (int, error) {
	length, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return 0, err
	}
	if length < -1 || length > int64(limit) {
		return 0, fmt.Errorf("length %d is outside [-1,%d]", length, limit)
	}
	return int(length), nil
}

func (value respValue) asError() error {
	if value.kind != respError {
		return nil
	}
	return fmt.Errorf("%w: %s", errRedis, value.errText)
}

func (value respValue) stringValue() (string, error) {
	switch value.kind {
	case respSimple, respBulk:
		return string(value.data), nil
	case respInteger:
		return strconv.FormatInt(value.integer, 10), nil
	case respNil:
		return "", errors.New("RESP value is nil")
	default:
		return "", fmt.Errorf("RESP value kind %q is not a string", value.kind)
	}
}

func (value respValue) int64Value() (int64, error) {
	if value.kind == respInteger {
		return value.integer, nil
	}
	text, err := value.stringValue()
	if err != nil {
		return 0, err
	}
	integer, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("RESP value %q is not an integer: %w", text, err)
	}
	return integer, nil
}

func (value respValue) stringSlice() ([]string, error) {
	if value.kind == respNil {
		return nil, nil
	}
	if value.kind != respArray {
		return nil, fmt.Errorf("RESP value kind %q is not an array", value.kind)
	}
	stringsValue := make([]string, 0, len(value.items))
	for _, item := range value.items {
		if item.kind == respNil {
			continue
		}
		text, err := item.stringValue()
		if err != nil {
			return nil, err
		}
		stringsValue = append(stringsValue, text)
	}
	return stringsValue, nil
}

func doConn(connection net.Conn, args ...string) (respValue, error) {
	if _, err := connection.Write(encodeCommand(args...)); err != nil {
		return respValue{}, err
	}
	return readRESP(bufio.NewReader(connection))
}

type redisClient struct {
	connection net.Conn
	reader     *bufio.Reader
	mutex      sync.Mutex
}

func dialRedis(ctx context.Context, address, password string, database int) (*redisClient, error) {
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	client := &redisClient{connection: connection, reader: bufio.NewReader(connection)}
	if password != "" {
		if _, err := client.do(ctx, "AUTH", password); err != nil {
			connection.Close()
			return nil, err
		}
	}
	if database != 0 {
		if _, err := client.do(ctx, "SELECT", strconv.Itoa(database)); err != nil {
			connection.Close()
			return nil, err
		}
	}
	return client, nil
}

func (client *redisClient) do(ctx context.Context, args ...string) (respValue, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		if err := client.connection.SetDeadline(deadline); err != nil {
			return respValue{}, err
		}
	} else if err := client.connection.SetDeadline(time.Time{}); err != nil {
		return respValue{}, err
	}
	if _, err := client.connection.Write(encodeCommand(args...)); err != nil {
		return respValue{}, err
	}
	value, err := readRESP(client.reader)
	if err != nil {
		return respValue{}, err
	}
	if err := value.asError(); err != nil {
		return value, err
	}
	return value, nil
}

func (client *redisClient) close() error {
	if client == nil || client.connection == nil {
		return nil
	}
	return client.connection.Close()
}
