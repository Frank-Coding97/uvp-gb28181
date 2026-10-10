package handler

import (
	"testing"

	"github.com/emiago/sipgo/sip"
)

func TestParseExpires_ContactParameterOverridesHeader(t *testing.T) {
	req := sip.NewRequest(sip.REGISTER, sip.Uri{Host: "platform"})
	req.AppendHeader(sip.NewHeader("Contact", "<sip:34020000001320000001@127.0.0.1>;expires=0"))
	req.AppendHeader(sip.NewHeader("Expires", "3600"))

	if got := parseExpires(req); got != 0 {
		t.Fatalf("Contact expires=0 应覆盖 Expires:3600,实际 %d", got)
	}
}
