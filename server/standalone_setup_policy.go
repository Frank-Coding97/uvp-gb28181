package main

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"
)

func validStandaloneAdmin(username, password string, minimum int, special bool) bool {
	username = strings.TrimSpace(username)
	if !utf8.ValidString(username) || username == "" || utf8.RuneCountInString(username) > 50 {
		return false
	}
	if minimum < 6 {
		minimum = 6
	}
	return len(password) >= minimum && len(password) <= 72 && (!special || strings.ContainsAny(password, "!@#$%"))
}

// Match the launcher's local probe URL even when the listener uses a wildcard.
func standaloneSetupOrigin(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	number, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || number < 1 || number > 65535 {
		return "", errors.New("invalid standalone management address")
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	if !net.ParseIP(host).IsLoopback() {
		return "", errors.New("standalone setup requires a local management origin")
	}
	return "http://" + net.JoinHostPort(host, port), nil
}
