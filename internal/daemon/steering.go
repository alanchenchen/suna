package daemon

import (
	"context"
	"strings"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/protocol"
)

func (s *service) handleSteer(ctx context.Context, req protocol.Request) (protocol.SteerResult, error) {
	var params protocol.SteerParams
	if err := decodeParams(req.Params, &params); err != nil {
		return protocol.SteerResult{}, invalidParams(err.Error())
	}
	rt, sessionID, err := s.daemon.sessions.steerableRun(req.ConnID, params.RunID)
	if err != nil {
		return protocol.SteerResult{}, invalidParams(err.Error())
	}
	text, err := steeringText(params.Parts)
	if err != nil {
		return protocol.SteerResult{}, invalidParams(err.Error())
	}
	item, created, err := rt.agent.EnqueueSteering(params.RunID, params.ClientMsgID, text)
	if err != nil {
		return protocol.SteerResult{}, invalidParams(err.Error())
	}
	message := steeringMessage(item, params.RunID, true)
	if created {
		s.emitSteering(ctx, sessionID, req.ConnID, message)
	}
	return protocol.SteerResult{Message: message}, nil
}

func (s *service) handleSteerRemove(ctx context.Context, req protocol.Request) (protocol.SteerResult, error) {
	var params protocol.SteerRemoveParams
	if err := decodeParams(req.Params, &params); err != nil {
		return protocol.SteerResult{}, invalidParams(err.Error())
	}
	rt, sessionID, err := s.daemon.sessions.steerableRun(req.ConnID, params.RunID)
	if err != nil {
		return protocol.SteerResult{}, invalidParams(err.Error())
	}
	item, removed, err := rt.agent.RemoveSteering(params.RunID, params.ID)
	if err != nil {
		return protocol.SteerResult{}, invalidParams(err.Error())
	}
	message := steeringMessage(item, params.RunID, true)
	if removed {
		s.emitSteering(ctx, sessionID, req.ConnID, message)
	}
	return protocol.SteerResult{Message: message}, nil
}

func steeringText(parts []protocol.MessagePart) (string, error) {
	var texts []string
	for _, part := range parts {
		if part.Type != "text" {
			return "", protocolError{code: -32602, message: "steering supports text parts only", data: protocol.ProtocolErrorData{Kind: "unsupported_content"}}
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			texts = append(texts, text)
		}
	}
	text := strings.TrimSpace(strings.Join(texts, "\n"))
	if text == "" {
		return "", protocolError{code: -32602, message: "content is required", data: protocol.ProtocolErrorData{Kind: "invalid_request"}}
	}
	return text, nil
}

func steeringMessage(item agent.SteeringItem, runID string, canControl bool) protocol.SteeringMessage {
	state := protocol.SteeringState(item.State)
	return protocol.SteeringMessage{ID: item.ID, RunID: runID, ClientMsgID: item.ClientMsgID, State: state, Sequence: item.Sequence, CanControl: canControl, Parts: []protocol.MessagePart{{Type: "text", Text: item.Text}}}
}

func (s *service) emitSteering(ctx context.Context, sessionID, ownerID string, message protocol.SteeringMessage) {
	for _, targetConnID := range s.daemon.sessions.connIDsForSession(sessionID) {
		p := message
		p.CanControl = targetConnID == ownerID
		emit(ctx, s.daemon.sinkFor(targetConnID, nil), protocol.NotifySteering, p)
	}
}
