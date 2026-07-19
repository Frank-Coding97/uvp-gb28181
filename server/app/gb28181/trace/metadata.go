package trace

import (
	"fmt"

	"github.com/emiago/sipgo/sip"
)

type SIPMetadata struct {
	DeviceID   string
	Method     string
	StatusCode uint16
	CallID     string
	CSeq       uint32
	CSeqMethod string
	ParseError string
}

func extractSIPMetadata(raw []byte, direction Direction) SIPMetadata {
	message, err := sip.NewParser().ParseSIP(raw)
	if err != nil {
		return SIPMetadata{ParseError: "parse SIP message failed"}
	}
	metadata := SIPMetadata{}
	if callID := message.CallID(); callID != nil {
		metadata.CallID = string(*callID)
	}
	if cseq := message.CSeq(); cseq != nil {
		metadata.CSeq = cseq.SeqNo
		metadata.CSeqMethod = cseq.MethodName.String()
	}

	switch typed := message.(type) {
	case *sip.Request:
		metadata.Method = typed.Method.String()
		if direction == DirectionOutbound {
			metadata.DeviceID = typed.Recipient.User
		} else if from := typed.From(); from != nil {
			metadata.DeviceID = from.Address.User
		}
	case *sip.Response:
		metadata.Method = metadata.CSeqMethod
		if typed.StatusCode >= 0 && typed.StatusCode <= int(^uint16(0)) {
			metadata.StatusCode = uint16(typed.StatusCode)
		}
		if direction == DirectionOutbound {
			if to := typed.To(); to != nil {
				metadata.DeviceID = to.Address.User
			}
		} else if from := typed.From(); from != nil {
			metadata.DeviceID = from.Address.User
		}
	default:
		metadata.ParseError = fmt.Sprintf("unsupported SIP message type")
	}
	return metadata
}
