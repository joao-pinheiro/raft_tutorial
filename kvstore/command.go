package kvstore

import "encoding/json"

// CommandType identifies the kind of KV operation.
type CommandType int

const (
	CmdSet CommandType = iota
	CmdDelete
)

// Command represents a KV store operation.
type Command struct {
	Type  CommandType `json:"type"`
	Key   string      `json:"key"`
	Value string      `json:"value,omitempty"`
}

// Encode serializes a Command to JSON bytes.
func (c Command) Encode() ([]byte, error) {
	return json.Marshal(c)
}

// DecodeCommand deserializes a Command from JSON bytes.
func DecodeCommand(data []byte) (Command, error) {
	var cmd Command
	err := json.Unmarshal(data, &cmd)
	return cmd, err
}
