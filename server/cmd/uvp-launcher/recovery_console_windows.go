//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func readRecoveryConfirmationInput(info standalone.RecoveryConfirmationInfo, out io.Writer) (standalone.RecoveryConfirmationInput, error) {
	var input standalone.RecoveryConfirmationInput
	if out == nil {
		return input, errors.New("recovery confirmation output is unavailable")
	}
	if _, err := fmt.Fprint(out, "管理员用户名："); err != nil {
		return input, err
	}
	username, err := readRecoveryConsoleLine(true)
	if err != nil {
		return input, err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return input, err
	}
	if _, err := fmt.Fprint(out, "管理员密码："); err != nil {
		return input, err
	}
	password, err := readRecoveryConsoleLine(false)
	if err != nil {
		return input, err
	}
	// The password was read with console echo disabled. Emit only the line
	// ending after the mode has been restored; never print the secret.
	if _, err := fmt.Fprintln(out); err != nil {
		return input, err
	}
	if _, err := fmt.Fprintf(out, "请输入确认串 CONFIRM %s：", info.OperationID); err != nil {
		return input, err
	}
	acknowledgement, err := readRecoveryConsoleLine(true)
	if err != nil {
		return input, err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return input, err
	}
	input.Username = username
	input.Password = password
	input.Acknowledgement = acknowledgement
	return input, nil
}

func readRecoveryConsoleLine(echo bool) (line string, err error) {
	handle := windows.Handle(os.Stdin.Fd())
	var original uint32
	if err := windows.GetConsoleMode(handle, &original); err != nil {
		return "", errors.New("本机确认必须在 Windows 控制台中执行")
	}
	mode := original | windows.ENABLE_LINE_INPUT
	if echo {
		mode |= windows.ENABLE_ECHO_INPUT
	} else {
		mode &^= windows.ENABLE_ECHO_INPUT
	}
	if err := windows.SetConsoleMode(handle, mode); err != nil {
		return "", errors.New("无法设置 Windows 控制台输入模式")
	}
	defer func() {
		if restoreErr := windows.SetConsoleMode(handle, original); restoreErr != nil {
			line = ""
			err = errors.New("无法恢复 Windows 控制台输入模式")
		}
	}()
	buffer := make([]uint16, 4096)
	var read uint32
	if readErr := windows.ReadConsole(handle, &buffer[0], uint32(len(buffer)), &read, nil); readErr != nil {
		return "", errors.New("无法读取 Windows 控制台输入")
	}
	if read > uint32(len(buffer)) {
		return "", errors.New("Windows 控制台输入长度无效")
	}
	decoded := string(utf16.Decode(buffer[:read]))
	if !strings.ContainsAny(decoded, "\r\n") {
		if flushErr := windows.FlushConsoleInputBuffer(handle); flushErr != nil {
			return "", errors.New("Windows 控制台输入过长且无法清理")
		}
		return "", errors.New("Windows 控制台输入过长")
	}
	return strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(decoded, "\r\n"), "\n"), "\r"), nil
}
