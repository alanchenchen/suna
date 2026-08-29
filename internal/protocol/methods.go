package protocol

const (
	MethodRuntimeHello = "runtime.hello"
)

const (
	MethodSendMessage = "agent.sendMessage"
	MethodSteer       = "agent.steer"
	MethodSteerRemove = "agent.steerRemove"
	MethodResumeRun   = "agent.resumeRun"
	MethodCancel      = "agent.cancel"
	MethodAskReply    = "agent.askReply"
	MethodGuardReply  = "agent.guardReply"
)

const (
	MethodSessionList   = "session.list"
	MethodSessionCreate = "session.create"
	MethodSessionAttach = "session.attach"
	MethodSessionDetach = "session.detach"
	MethodSessionUpdate = "session.update"
	MethodSessionDelete = "session.delete"
	MethodCompact       = "session.compact"
	MethodUsage         = "session.usage"
)

const (
	MethodConfigGet            = "config.get"
	MethodConfigSet            = "config.set"
	MethodConfigDiscoverModels = "config.discoverModels"
)

const (
	MethodAttachmentStatus = "attachment.status"
	MethodAttachmentClear  = "attachment.clear"
)

const (
	MethodDaemonStatus = "daemon.status"
	MethodDaemonStop   = "daemon.stop"
	MethodDebugMemory  = "debug.memory"
)

const (
	MethodMemoryList   = "memory.list"
	MethodMemoryDelete = "memory.delete"
	MethodMemoryClear  = "memory.clear"
	MethodSkillList    = "skill.list"
	MethodSkillSet     = "skill.set"
	MethodMCPList      = "mcp.list"
	MethodMCPToggle    = "mcp.toggle"
	MethodMCPReload    = "mcp.reload"
)

// Reserved for future trigger/skill runtime support. These methods are not handled by daemon service yet.
const (
	MethodTriggerList   = "trigger.list"
	MethodTriggerAdd    = "trigger.add"
	MethodTriggerRemove = "trigger.remove"
)

const (
	ConfigActionUpsertModel   = "upsert_model"
	ConfigActionDeleteModel   = "delete_model"
	ConfigActionActivateModel = "activate_model"
	ConfigActionUpdateGeneral = "update_general"
)

const (
	NotifyAgentDelta          = "agent.delta"
	NotifyAgentRun            = "agent.run"
	NotifySteering            = "agent.steering"
	NotifySessionUserMessage  = "session.user_message"
	NotifySessionUpdated      = "session.updated"
	NotifyUsage               = "agent.usage"
	NotifyToolStart           = "agent.tool_start"
	NotifyToolGuard           = "agent.tool_guard"
	NotifyToolEnd             = "agent.tool_end"
	NotifyAskUser             = "agent.ask_user"
	NotifyGuardConfirm        = "agent.guard_confirm"
	NotifyInteractionResolved = "agent.interaction_resolved"
)

const (
	NotifyDaemonState      = "daemon.state"
	NotifyDaemonFullStatus = "daemon.full_status"
	NotifyMCPUpdated       = "mcp.updated"
)

const (
	NotifyConfigState = "config.state"
)

const (
	NotifyCompactResult = "session.compact_result"
)

const (
	NotifyMemoryState = "memory.state"
	NotifySkillLoad   = "skill.load"
	NotifySkillReview = "skill.review"
)

// Reserved for future perception notifications. It is not emitted by the current runtime.
const (
	NotifyPerception = "perception.event"
)

const (
	AttachmentKindPath       = "path"
	AttachmentKindURL        = "url"
	AttachmentKindAttachment = "attachment"
)
