package server

import (
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// jsonCodec replaces connect's default "json" codec so that responses emit
// default-valued scalar fields (e.g. a false Task.active) instead of
// omitting them. protojson's default MarshalOptions treats a zero value as
// unpopulated and drops it from the JSON entirely, which is indistinguishable
// on the wire from a field that was never set — the frontend can't tell "this
// task is paused" (active: false) from "this task doesn't have an active
// field" (also seen as active: undefined) unless the zero value is emitted.
type jsonCodec struct{}

// JSONCodecOption overrides connect's default JSON codec with jsonCodec.
func JSONCodecOption() connect.Option {
	return connect.WithCodec(jsonCodec{})
}

func (jsonCodec) Name() string { return "json" }

func (jsonCodec) Marshal(message any) ([]byte, error) {
	protoMessage, ok := message.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("%T doesn't implement proto.Message", message)
	}
	return protojson.MarshalOptions{EmitDefaultValues: true}.Marshal(protoMessage)
}

func (jsonCodec) Unmarshal(data []byte, message any) error {
	protoMessage, ok := message.(proto.Message)
	if !ok {
		return fmt.Errorf("%T doesn't implement proto.Message", message)
	}
	if len(data) == 0 {
		return errors.New("zero-length payload is not a valid JSON object")
	}
	options := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := options.Unmarshal(data, protoMessage); err != nil {
		return fmt.Errorf("unmarshal into %T: %w", message, err)
	}
	return nil
}
