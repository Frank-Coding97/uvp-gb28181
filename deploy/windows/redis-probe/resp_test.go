package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
)

func TestEncodeCommandUsesRESPBulkArguments(t *testing.T) {
	got := encodeCommand("SET", "中文 key", "value with spaces", "")
	want := "*4\r\n$3\r\nSET\r\n$10\r\n中文 key\r\n$17\r\nvalue with spaces\r\n$0\r\n\r\n"
	if string(got) != want {
		t.Fatalf("encoded command = %q, want %q", got, want)
	}
}

func TestReadRESP2NestedValuesAndNil(t *testing.T) {
	input := "+OK\r\n:42\r\n$5\r\nhello\r\n$-1\r\n*2\r\n:1\r\n$3\r\nfoo\r\n"
	r := bufio.NewReader(bytes.NewBufferString(input))

	ok, err := readRESP(r)
	if err != nil || ok.kind != respSimple || string(ok.data) != "OK" {
		t.Fatalf("simple response = %#v, err=%v", ok, err)
	}
	integer, err := readRESP(r)
	if err != nil || integer.kind != respInteger || integer.integer != 42 {
		t.Fatalf("integer response = %#v, err=%v", integer, err)
	}
	bulk, err := readRESP(r)
	if err != nil || bulk.kind != respBulk || string(bulk.data) != "hello" {
		t.Fatalf("bulk response = %#v, err=%v", bulk, err)
	}
	nilValue, err := readRESP(r)
	if err != nil || nilValue.kind != respNil {
		t.Fatalf("nil response = %#v, err=%v", nilValue, err)
	}
	array, err := readRESP(r)
	if err != nil || array.kind != respArray || len(array.items) != 2 {
		t.Fatalf("array response = %#v, err=%v", array, err)
	}
	if array.items[0].integer != 1 || string(array.items[1].data) != "foo" {
		t.Fatalf("array items = %#v", array.items)
	}
}

func TestReadRESP2ErrorIsTyped(t *testing.T) {
	value, err := readRESP(bufio.NewReader(bytes.NewBufferString("-ERR wrong password\r\n")))
	if err != nil {
		t.Fatalf("read error response: %v", err)
	}
	if value.kind != respError || value.errText != "ERR wrong password" {
		t.Fatalf("error response = %#v", value)
	}
	if !errors.Is(value.asError(), errRedis) {
		t.Fatalf("asError() = %v, want redis error", value.asError())
	}
}

func TestReadRESP2RejectsMalformedLengthAndTruncatedFrame(t *testing.T) {
	for name, input := range map[string]string{
		"negative array length": "*-2\r\n",
		"invalid bulk length":   "$x\r\n",
		"truncated bulk":        "$5\r\nabc",
		"missing line ending":   "+OK\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readRESP(bufio.NewReader(bytes.NewBufferString(input)))
			if err == nil || errors.Is(err, io.EOF) && name == "invalid bulk length" {
				t.Fatalf("readRESP(%q) err=%v, want parse/truncation error", input, err)
			}
		})
	}
}

func TestClientDoWritesOneCommandAndReadsResponse(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(server)
		line, err := reader.ReadString('\n')
		if err != nil {
			done <- err
			return
		}
		if line != "*2\r\n" {
			done <- errors.New("unexpected array header: " + line)
			return
		}
		first, err := readRESP(reader)
		if err != nil || string(first.data) != "PING" {
			done <- errors.New("unexpected first argument")
			return
		}
		second, err := readRESP(reader)
		if err != nil || string(second.data) != "hello" {
			done <- errors.New("unexpected second argument")
			return
		}
		_, err = server.Write([]byte("+PONG\r\n"))
		done <- err
	}()

	got, err := doConn(client, "PING", "hello")
	if err != nil {
		t.Fatalf("doConn: %v", err)
	}
	if got.kind != respSimple || string(got.data) != "PONG" {
		t.Fatalf("response = %#v", got)
	}
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestRESPStringAndArrayConversions(t *testing.T) {
	value := respValue{kind: respBulk, data: []byte("7")}
	if got, err := value.stringValue(); err != nil || got != "7" {
		t.Fatalf("stringValue = %q, %v", got, err)
	}
	if got, err := value.int64Value(); err != nil || got != 7 {
		t.Fatalf("int64Value = %d, %v", got, err)
	}
	array := respValue{kind: respArray, items: []respValue{{kind: respBulk, data: []byte("one")}, {kind: respNil}}}
	if got, err := array.stringSlice(); err != nil || !reflect.DeepEqual(got, []string{"one"}) {
		t.Fatalf("stringSlice = %#v, %v", got, err)
	}
}
