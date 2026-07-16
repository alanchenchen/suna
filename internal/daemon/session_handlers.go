package daemon

import (
	"context"
	"strings"

	"github.com/alanchenchen/suna/internal/protocol"
)

func (s *service) requireSession(connID string) (*sessionRuntime, string, error) {
	rt, id, err := s.daemon.sessions.attachedSession(connID)
	if err != nil {
		return nil, "", protocolError{code: -32602, message: err.Error(), data: protocol.ProtocolErrorData{Kind: "session_required"}}
	}
	return rt, id, nil
}

func (s *service) handleSessionList(ctx context.Context, req protocol.Request) (any, error) {
	var params protocol.SessionListParams
	if req.Params != nil {
		if err := decodeParams(req.Params, &params); err != nil {
			return nil, invalidParams(err.Error())
		}
	}
	items, err := s.daemon.sessions.list(ctx, params.ActiveOnly)
	if err != nil {
		return nil, protocolError{code: -32603, message: err.Error()}
	}
	if params.CWD != "" {
		cwd := canonicalCWD(params.CWD)
		filtered := items[:0]
		for _, item := range items {
			if canonicalCWD(item.CWD) == cwd {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	return protocol.SessionListResult{Sessions: items}, nil
}

func (s *service) handleSessionCreate(ctx context.Context, req protocol.Request) (any, error) {
	var params protocol.SessionCreateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	if strings.TrimSpace(params.CWD) == "" {
		return nil, invalidParams("cwd is required")
	}
	oldSessionID := s.daemon.sessions.currentSessionID(req.ConnID)
	snapshot, err := s.daemon.sessions.create(ctx, req.ConnID, params.CWD, params.Title)
	if err != nil {
		return nil, protocolError{code: -32603, message: err.Error()}
	}
	if oldSessionID != "" && oldSessionID != snapshot.Session.ID {
		s.broadcastSessionState(ctx, oldSessionID)
	}
	s.broadcastSessionState(ctx, snapshot.Session.ID)
	return snapshot, nil
}

func (s *service) handleSessionAttach(ctx context.Context, req protocol.Request) (any, error) {
	var params protocol.SessionAttachParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, invalidParams("session_id is required")
	}
	oldSessionID := s.daemon.sessions.currentSessionID(req.ConnID)
	snapshot, err := s.daemon.sessions.attach(ctx, req.ConnID, params.SessionID, params.RequireActive)
	if err != nil {
		return nil, protocolError{code: -32603, message: err.Error()}
	}
	if oldSessionID != "" && oldSessionID != snapshot.Session.ID {
		s.broadcastSessionState(ctx, oldSessionID)
	}
	s.broadcastSessionState(ctx, snapshot.Session.ID)
	return snapshot, nil
}

func (s *service) handleSessionDetach(ctx context.Context, req protocol.Request) (any, error) {
	sessionID := s.daemon.sessions.detach(req.ConnID)
	if sessionID != "" {
		s.onClientDetached(ctx, req.ConnID, sessionID)
		s.broadcastSessionState(ctx, sessionID)
	}
	return map[string]string{"status": "detached"}, nil
}

func (s *service) handleSessionUpdate(ctx context.Context, req protocol.Request) (any, error) {
	var params protocol.SessionUpdateParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, invalidParams("session_id is required")
	}
	updated, err := s.daemon.sessions.update(ctx, req.ConnID, params)
	if err != nil {
		return nil, protocolError{code: -32603, message: err.Error()}
	}
	s.daemon.broadcastSessionState(ctx, updated.Session.ID)
	return updated, nil
}

func (s *service) handleSessionDelete(ctx context.Context, req protocol.Request) (any, error) {
	var params protocol.SessionDeleteParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return nil, invalidParams("session_id is required")
	}
	if err := s.daemon.sessions.delete(ctx, req.ConnID, params.SessionID); err != nil {
		return nil, protocolError{code: -32603, message: err.Error()}
	}
	return protocol.SessionDeleteResult{Deleted: true}, nil
}
