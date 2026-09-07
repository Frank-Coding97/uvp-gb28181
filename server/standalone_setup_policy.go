package main

import (
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
