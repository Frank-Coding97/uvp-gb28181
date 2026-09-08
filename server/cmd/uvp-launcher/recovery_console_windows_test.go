//go:build windows

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

// Exercise actual Windows console input, rather than replacing the password
// reader with a callback. Only this test process's console is created/freed.
func TestWindowsRecoveryConsolePasswordNotEchoed(t *testing.T) {
	if os.Getenv("UVP_RECOVERY_CONSOLE_TEST") != "1" {
		t.Skip("requires isolated native console test")
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	freeConsole := kernel.NewProc("FreeConsole")
	_, _, _ = freeConsole.Call()
	result, _, err := kernel.NewProc("AllocConsole").Call()
	if result == 0 {
		t.Fatal("allocate isolated console:", err)
	}
	defer freeConsole.Call()
	openConsole := func(name string) windows.Handle {
		path, err := windows.UTF16PtrFromString(name)
		if err != nil {
			t.Fatal(err)
		}
		h, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
		if err != nil {
			t.Fatal("open isolated console:", err)
		}
		return h
	}
	inputHandle, outputHandle := openConsole("CONIN$"), openConsole("CONOUT$")
	defer windows.CloseHandle(outputHandle)
	inputFile := os.NewFile(uintptr(inputHandle), "isolated-console")
	defer inputFile.Close()
	previousInput := os.Stdin
	os.Stdin = inputFile
	defer func() { os.Stdin = previousInput }()
	var original uint32
	if err := windows.GetConsoleMode(inputHandle, &original); err != nil {
		t.Fatal(err)
	}
	writeInput := kernel.NewProc("WriteConsoleInputW")
	// INPUT_RECORD containing KEY_EVENT_RECORD, using Windows' 20-byte layout.
	type keyRecord struct {
		EventType  uint16
		Padding    uint16
		KeyDown    int32
		Repeat     uint16
		VirtualKey uint16
		Scan       uint16
		Character  uint16
		Control    uint32
	}
	writeLine := func(value string) error {
		for _, character := range utf16.Encode([]rune(value + "\r")) {
			record := keyRecord{EventType: 1, KeyDown: 1, Repeat: 1, Character: character}
			if character == '\r' {
				record.VirtualKey = 13
			}
			var written uint32
			ok, _, err := writeInput.Call(uintptr(inputHandle), uintptr(unsafe.Pointer(&record)), 1, uintptr(unsafe.Pointer(&written)))
			if ok == 0 || written != 1 {
				return err
			}
		}
		return nil
	}
	waitEcho := func(echo bool) bool {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var mode uint32
			if windows.GetConsoleMode(inputHandle, &mode) == nil && (mode&windows.ENABLE_ECHO_INPUT != 0) == echo {
				return true
			}
			time.Sleep(5 * time.Millisecond)
		}
		return false
	}
	const password = "ConsoleFixture!Password42"
	writerDone := make(chan error, 1)
	go func() {
		if err := writeLine("fixture-admin"); err != nil {
			writerDone <- err
			return
		}
		if !waitEcho(false) {
			writerDone <- os.ErrDeadlineExceeded
			return
		}
		if err := writeLine(password); err != nil {
			writerDone <- err
			return
		}
		if !waitEcho(true) {
			writerDone <- os.ErrDeadlineExceeded
			return
		}
		writerDone <- writeLine("CONFIRM fixture-operation")
	}()
	var prompt bytes.Buffer
	answer, err := readRecoveryConfirmationInput(standalone.RecoveryConfirmationInfo{OperationID: "fixture-operation"}, &prompt)
	if err != nil {
		t.Fatal("console prompt failed:", err)
	}
	if err := <-writerDone; err != nil {
		t.Fatal("inject console input:", err)
	}
	if answer.Username != "fixture-admin" || answer.Password != password || answer.Acknowledgement != "CONFIRM fixture-operation" {
		t.Fatal("console input was not preserved")
	}
	if strings.Contains(prompt.String(), password) {
		t.Fatal("password appeared in prompt output")
	}
	var restored uint32
	if err := windows.GetConsoleMode(inputHandle, &restored); err != nil || restored != original {
		t.Fatal("console mode was not restored")
	}
	buffer := make([]uint16, 2000)
	var read uint32
	ok, _, err := kernel.NewProc("ReadConsoleOutputCharacterW").Call(uintptr(outputHandle), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0, uintptr(unsafe.Pointer(&read)))
	if ok == 0 {
		t.Fatal("inspect isolated console:", err)
	}
	visible := string(utf16.Decode(buffer[:read]))
	if strings.Contains(visible, password) {
		t.Fatal("password was echoed to the Windows console")
	}
}
