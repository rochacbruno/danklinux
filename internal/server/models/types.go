package models

import (
	"encoding/json"
	"net"

	"github.com/AvengeMedia/danklinux/internal/log"
)

type Request struct {
	ID     interface{}            `json:"id,omitempty"`
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type Response[T any] struct {
	ID     interface{} `json:"id,omitempty"`
	Result *T          `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func RespondError(conn net.Conn, id interface{}, errMsg string) {
	log.Errorf("DMS API Error: id=%v error=%s", id, errMsg)
	resp := Response[any]{ID: id, Error: errMsg}
	json.NewEncoder(conn).Encode(resp)
}

func Respond[T any](conn net.Conn, id interface{}, result T) {
	resp := Response[T]{ID: id, Result: &result}
	json.NewEncoder(conn).Encode(resp)
}
