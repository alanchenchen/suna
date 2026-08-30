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
		return protocol.SteerResult{}, protocol.InvalidRequest(err.Error())
	}
	rt, sessionID, err := s.daemon.sessions.steerableRun(req.ConnID, params.RunID)
	if err != nil {
		return protocol.SteerResult{}, requestErrorForSteering(err)
	}
	text, err := steeringText(params.Parts)
	if err != nil {
		return protocol.SteerResult{}, err
	}
	item, created, err := rt.agent.EnqueueSteering(params.RunID, params.ClientMsgID, text)
	if err != nil {
		return protocol.SteerResult{}, requestErrorForSteering(err)
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
		return protocol.SteerResult{}, protocol.InvalidRequest(err.Error())
	}
	rt, sessionID, err := s.daemon.sessions.steerableRun(req.ConnID, params.RunID)
	if err != nil {
		return protocol.SteerResult{}, requestErrorForSteering(err)
	}
	item, removed, err := rt.agent.RemoveSteering(params.RunID, params.ID)
	if err != nil {
		return protocol.SteerResult{}, requestErrorForSteering(err)
	}
	message := steeringMessage(item, params.RunID, true)
	if removed {
		s.emitSteering(ctx, sessionID, req.ConnID, message)
	}
	return protocol.SteerResult{Message: message}, nil
}

func requestErrorForSteering(err error) *protocol.RequestError {
	if err == nil {
		return protocol.InternalError("internal error")
	}
	switch err.Error() {
	case "session_required", "session not loaded":
		return protocol.SessionRequired(err.Error())
	case "session_busy", "run_not_steerable":
		return protocol.SessionBusyReason(err.Error(), protocol.ErrorReasonRunNotSteerable)
	case "interaction_pending":
		return protocol.SessionBusyReason(err.Error(), protocol.ErrorReasonInteractionPending)
	case "client_msg_conflict":
		return protocol.InvalidRequestReason(err.Error(), protocol.ErrorReasonClientMsgConflict)
	case "steering_queue_full":
		return protocol.InvalidRequestReason(err.Error(), protocol.ErrorReasonSteeringQueueFull)
	case "steering_not_found":
		return protocol.InvalidRequestReason(err.Error(), protocol.ErrorReasonSteeringNotFound)
	default:
		return protocol.InvalidRequest(err.Error())
	}
}

func steeringText(parts []protocol.MessagePart) (string, error) {
	var texts []string
	for _, part := range parts {
		if part.Type != "text" {
			return "", protocol.InvalidRequest("steering supports text parts only")
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			texts = append(texts, text)
		}
	}
	text := strings.TrimSpace(strings.Join(texts, "\n"))
	if text == "" {
		return "", protocol.InvalidRequest("content is required")
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
		p.SessionID = sessionID
		p.CanControl = targetConnID == ownerID
		emit(ctx, s.daemon.sinkFor(targetConnID, nil), protocol.NotifySteering, p)
	}
}
