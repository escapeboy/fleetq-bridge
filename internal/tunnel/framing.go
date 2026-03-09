package tunnel

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// FrameType identifies the type of a tunnel frame.
type FrameType uint16

const (
	FrameLLMRequest      FrameType = 0x0001
	FrameLLMResponseChunk FrameType = 0x0002
	FrameLLMResponseEnd  FrameType = 0x0003
	FrameAgentRequest    FrameType = 0x0010
	FrameAgentEvent      FrameType = 0x0011
	FrameAgentDone       FrameType = 0x0012
	FrameMCPRequest      FrameType = 0x0020
	FrameMCPResponse     FrameType = 0x0021
	FrameDiscover        FrameType = 0x0030
	FrameDiscoverAck     FrameType = 0x0031
	FrameHeartbeat       FrameType = 0x00F0
	FrameHeartbeatAck    FrameType = 0x00F1
	FrameError           FrameType = 0x00FF
	FrameRotateKey       FrameType = 0x0100
)

// Frame is the envelope for all tunnel messages.
// Wire format: [4 bytes: request_id_len][request_id][2 bytes: frame_type][4 bytes: payload_len][payload]
type Frame struct {
	RequestID string
	Type      FrameType
	Payload   []byte
}

// Encode serialises a Frame into the wire format.
func (f *Frame) Encode(w io.Writer) error {
	idBytes := []byte(f.RequestID)

	if err := binary.Write(w, binary.BigEndian, uint32(len(idBytes))); err != nil {
		return err
	}
	if _, err := w.Write(idBytes); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, f.Type); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(f.Payload))); err != nil {
		return err
	}
	_, err := w.Write(f.Payload)
	return err
}

// Decode reads a Frame from the wire format.
func Decode(r io.Reader) (*Frame, error) {
	var idLen uint32
	if err := binary.Read(r, binary.BigEndian, &idLen); err != nil {
		return nil, err
	}
	if idLen > 256 {
		return nil, fmt.Errorf("request_id too long: %d", idLen)
	}
	idBytes := make([]byte, idLen)
	if _, err := io.ReadFull(r, idBytes); err != nil {
		return nil, err
	}

	var frameType FrameType
	if err := binary.Read(r, binary.BigEndian, &frameType); err != nil {
		return nil, err
	}

	var payloadLen uint32
	if err := binary.Read(r, binary.BigEndian, &payloadLen); err != nil {
		return nil, err
	}
	if payloadLen > 10*1024*1024 { // 10 MB max
		return nil, fmt.Errorf("payload too large: %d bytes", payloadLen)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	return &Frame{
		RequestID: string(idBytes),
		Type:      frameType,
		Payload:   payload,
	}, nil
}

// NewJSONFrame creates a frame with a JSON-encoded payload.
func NewJSONFrame(requestID string, frameType FrameType, payload any) (*Frame, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Frame{
		RequestID: requestID,
		Type:      frameType,
		Payload:   data,
	}, nil
}
