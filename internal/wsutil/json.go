package wsutil

import (
	"context"
	"encoding/json"

	"github.com/coder/websocket"
)

func ReadJSON[T any](ctx context.Context, conn *websocket.Conn) (T, error) {
	var zero T
	_, data, err := conn.Read(ctx)
	if err != nil {
		return zero, err
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return zero, err
	}
	return value, nil
}

func WriteJSON(ctx context.Context, conn *websocket.Conn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
