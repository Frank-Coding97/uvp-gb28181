package zlm

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// RuntimePlayers and RuntimeSessions bind a complete fresh snapshot to the
// process that supplied it. Callers must compare BootNonce with the persisted
// viewer identity; a different boot is not evidence of disconnection.
type RuntimePlayers struct {
	BootNonce string
	Players   []MediaPlayer
}
type RuntimeSessions struct {
	BootNonce string
	Sessions  []Session
}

func (c *Client) GetRuntimeMediaPlayers(ctx context.Context, target StreamTarget) (RuntimePlayers, error) {
	if target.Validate() != nil {
		return RuntimePlayers{}, ErrRuntimeControlUnavailable
	}
	query := url.Values{"schema": {target.Schema}, "vhost": {target.VHost}, "app": {target.App}, "stream": {target.Stream}}
	boot, rows, err := c.runtimeRows(ctx, CapabilityGetMediaPlayerList, query)
	if err != nil {
		return RuntimePlayers{}, err
	}
	result := RuntimePlayers{BootNonce: boot, Players: make([]MediaPlayer, 0, len(rows))}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		var player MediaPlayer
		if err := runtimeRowKeys(row, "identifier"); err != nil {
			return RuntimePlayers{}, err
		}
		if json.Unmarshal(row, &player) != nil || !validRuntimeSessionID(player.Identifier) || seen[player.Identifier] {
			return RuntimePlayers{}, ErrRuntimeControlUnavailable
		}
		seen[player.Identifier] = true
		result.Players = append(result.Players, player)
	}
	return result, nil
}

func (c *Client) GetRuntimeSessions(ctx context.Context) (RuntimeSessions, error) {
	boot, rows, err := c.runtimeRows(ctx, CapabilityGetAllSession, nil)
	if err != nil {
		return RuntimeSessions{}, err
	}
	result := RuntimeSessions{BootNonce: boot, Sessions: make([]Session, 0, len(rows))}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		var session Session
		if err := runtimeRowKeys(row, "id", "identifier", "type"); err != nil {
			return RuntimeSessions{}, err
		}
		if json.Unmarshal(row, &session) != nil || !validRuntimeSessionID(session.ID) || session.Identifier != session.ID || seen[session.ID] || (session.Type != "tcp" && session.Type != "udp") {
			return RuntimeSessions{}, ErrRuntimeControlUnavailable
		}
		seen[session.ID] = true
		result.Sessions = append(result.Sessions, session)
	}
	return result, nil
}

// encoding/json accepts case-insensitive struct keys. Security identity must
// instead use the exact wire names, without case-variant shadow fields.
func runtimeRowKeys(row []byte, required ...string) error {
	fields, err := runtimeObject(row)
	if err != nil {
		return err
	}
	for _, name := range required {
		if fields[name] == nil {
			return ErrRuntimeControlUnavailable
		}
		for key := range fields {
			if key != name && strings.EqualFold(key, name) {
				return ErrRuntimeControlUnavailable
			}
		}
	}
	return nil
}

func (c *Client) runtimeRows(ctx context.Context, api string, query url.Values) (string, []json.RawMessage, error) {
	envelope, err := c.runtimeEnvelope(ctx, api, query, nil, 1024*1024)
	var boot string
	var rows []json.RawMessage
	if err != nil || json.Unmarshal(envelope["bootNonce"], &boot) != nil || !validRuntimeNonce(boot) || json.Unmarshal(envelope["data"], &rows) != nil || rows == nil {
		return "", nil, ErrRuntimeControlUnavailable
	}
	return boot, rows, nil
}
