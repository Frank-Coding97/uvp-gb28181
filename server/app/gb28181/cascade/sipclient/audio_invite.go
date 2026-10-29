package sipclient

import (
	"fmt"
	"strings"

	"github.com/emiago/sipgo/sip"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
)

// BuildAudioPull requests the original broadcaster's audio through this
// platform's configured upstream proxy. Source and published receiver belong
// to the broadcast binding, while From/Contact/Via remain platform-owned.
func (f *RequestFactory) BuildAudioPull(sourceID, receiverID string, body []byte, callID string) (*sip.Request, error) {
	if !validGBID(sourceID) || !validGBID(receiverID) || strings.ContainsAny(callID, "\r\n\x00") {
		return nil, fmt.Errorf("invalid cascade audio pull identity")
	}
	offer, err := sdp.ParseCascadeAudioOffer(body)
	if err != nil {
		return nil, err
	}
	if offer.SessionName != "Play" || offer.Direction != "recvonly" {
		return nil, fmt.Errorf("cascade audio pull requires Play/recvonly")
	}
	request, err := f.BuildMessage(body, callID)
	if err != nil {
		return nil, err
	}
	request.Method = sip.INVITE
	request.CSeq().MethodName = sip.INVITE
	request.Recipient.User = sourceID
	request.To().Address.User = sourceID
	request.ReplaceHeader(sip.NewHeader("Content-Type", "application/sdp"))
	request.AppendHeader(sip.NewHeader("Subject", sourceID+":"+offer.SSRC+","+receiverID+":0"))
	return request, nil
}
