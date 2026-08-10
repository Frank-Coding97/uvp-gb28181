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
	FromURI    string
	ToURI      string
	UserAgent  string
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
	if from := message.From(); from != nil {
		metadata.FromURI = fromToString(from.Address)
	}
	if to := message.To(); to != nil {
		metadata.ToURI = fromToString(to.Address)
	}
	if uas := message.GetHeaders("User-Agent"); len(uas) > 0 {
		metadata.UserAgent = truncateHeader(uas[0].Value(), 256)
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
			if from := typed.From(); from != nil {
				metadata.DeviceID = from.Address.User
			}
		} else if to := typed.To(); to != nil {
			metadata.DeviceID = to.Address.User
		}
	default:
		metadata.ParseError = fmt.Sprintf("unsupported SIP message type")
	}
	return metadata
}

// fromToString 序列化 From/To 头 URI 为 "user@host" 形式,超长截断避免日志存储压力
func fromToString(addr sip.Uri) string {
	s := addr.String()
	return truncateHeader(s, 256)
}

func truncateHeader(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
