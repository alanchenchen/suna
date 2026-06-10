package protocol

const (
	MethodSendMessage = "agent.sendMessage"
	MethodResumeRun   = "agent.resumeRun"
	MethodCancel      = "agent.cancel"
	MethodAskReply    = "agent.askReply"
	MethodGuardReply  = "agent.guardReply"
)

const (
	MethodSessionNew     = "session.new"
	MethodSessionRestore = "session.restore"
	MethodCompact        = "session.compact"
	MethodUsage          = "session.usage"
)

const (
	MethodConfigGet = "config.get"
	MethodConfigSet = "config.set"
)

const (
	MethodAttachmentStatus = "attachment.status"
	MethodAttachmentClear  = "attachment.clear"
)

const (
	MethodDaemonStatus = "daemon.status"
	MethodDaemonStop   = "daemon.stop"
)

const (
	MethodMemoryList = "memory.list"
	MethodSkillList  = "skill.list"
	MethodSkillSet   = "skill.set"
	MethodMCPList    = "mcp.list"
	MethodMCPToggle  = "mcp.toggle"
	MethodMCPReload  = "mcp.reload"
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
	NotifyStream       = "agent.stream"
	NotifyReasoning    = "agent.reasoning"
	NotifyUsage        = "agent.usage"
	NotifyToolStart    = "agent.tool_start"
	NotifyToolGuard    = "agent.tool_guard"
	NotifyToolEnd      = "agent.tool_end"
	NotifyAskUser      = "agent.ask_user"
	NotifyGuardConfirm = "agent.guard_confirm"
)

const (
	NotifyDaemonState      = "daemon.state"
	NotifyDaemonFullStatus = "daemon.full_status"
)

const (
	NotifyConfigState = "config.state"
)

const (
	NotifyCompactResult        = "session.compact_result"
	NotifySessionRestoreMsg    = "session.restore_message"
	NotifySessionRestoreStatus = "session.restore_status"
)

const (
	NotifyMemoryListResult = "memory.list_result"
	NotifySkillLoad        = "skill.load"
	NotifySkillReview      = "skill.review"
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
