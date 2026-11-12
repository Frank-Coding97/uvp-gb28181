package audio

import (
	"fmt"

	"github.com/pion/rtcp"
)

func rewriteRTCP(raw []byte, fromSSRC, toSSRC uint32) ([]byte, error) {
	if fromSSRC == 0 || toSSRC == 0 || fromSSRC == toSSRC {
		return nil, fmt.Errorf("invalid negotiated RTCP SSRC mapping")
	}
	packets, err := rtcp.Unmarshal(raw)
	if err != nil {
		return nil, fmt.Errorf("parse RTCP compound packet: %w", err)
	}
	for _, packet := range packets {
		switch p := packet.(type) {
		case *rtcp.SenderReport:
			if err = rewriteRTCPReporter(&p.SSRC, fromSSRC, toSSRC); err != nil {
				return nil, err
			}
			if err = rewriteRTCPReports(p.Reports, fromSSRC, toSSRC); err != nil {
				return nil, err
			}
		case *rtcp.ReceiverReport:
			if err = rewriteRTCPReporter(&p.SSRC, fromSSRC, toSSRC); err != nil {
				return nil, err
			}
			if err = rewriteRTCPReports(p.Reports, fromSSRC, toSSRC); err != nil {
				return nil, err
			}
		case *rtcp.SourceDescription:
			for i := range p.Chunks {
				if err = rewriteRTCPReporter(&p.Chunks[i].Source, fromSSRC, toSSRC); err != nil {
					return nil, err
				}
			}
		case *rtcp.Goodbye:
			for i := range p.Sources {
				if err = rewriteRTCPReporter(&p.Sources[i], fromSSRC, toSSRC); err != nil {
					return nil, err
				}
			}
		case *rtcp.TransportLayerNack:
			if err = rewriteRTCPFeedback(&p.SenderSSRC, &p.MediaSSRC, fromSSRC, toSSRC); err != nil {
				return nil, err
			}
		case *rtcp.PictureLossIndication:
			if err = rewriteRTCPFeedback(&p.SenderSSRC, &p.MediaSSRC, fromSSRC, toSSRC); err != nil {
				return nil, err
			}
		case *rtcp.FullIntraRequest:
			if err = rewriteRTCPFeedback(&p.SenderSSRC, &p.MediaSSRC, fromSSRC, toSSRC); err != nil {
				return nil, err
			}
			for i := range p.FIR {
				if err = rewriteRTCPReported(&p.FIR[i].SSRC, fromSSRC, toSSRC); err != nil {
					return nil, err
				}
			}
		default:
			return nil, fmt.Errorf("unsupported RTCP packet %T", packet)
		}
	}
	encoded, err := rtcp.Marshal(packets)
	if err != nil {
		return nil, fmt.Errorf("encode RTCP compound packet: %w", err)
	}
	return encoded, nil
}

func rewriteRTCPReporter(ssrc *uint32, fromSSRC, toSSRC uint32) error {
	if *ssrc != fromSSRC {
		return fmt.Errorf("RTCP sender SSRC does not match negotiated media")
	}
	*ssrc = toSSRC
	return nil
}

func rewriteRTCPReports(reports []rtcp.ReceptionReport, fromSSRC, toSSRC uint32) error {
	for i := range reports {
		if err := rewriteRTCPReported(&reports[i].SSRC, fromSSRC, toSSRC); err != nil {
			return err
		}
	}
	return nil
}

func rewriteRTCPReported(ssrc *uint32, fromSSRC, toSSRC uint32) error {
	if *ssrc != toSSRC {
		return fmt.Errorf("RTCP report SSRC does not match opposite media leg")
	}
	*ssrc = fromSSRC
	return nil
}

func rewriteRTCPFeedback(senderSSRC, mediaSSRC *uint32, fromSSRC, toSSRC uint32) error {
	if err := rewriteRTCPReporter(senderSSRC, fromSSRC, toSSRC); err != nil {
		return err
	}
	return rewriteRTCPReported(mediaSSRC, fromSSRC, toSSRC)
}
