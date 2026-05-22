package protocol

const (
	MethodSendMessage    = "agent.sendMessage"
	MethodCancel         = "agent.cancel"
	MethodGuardReply     = "agent.guardReply"
	MethodMemoryList     = "memory.list"
	MethodTriggerList    = "trigger.list"
	MethodTriggerAdd     = "trigger.add"
	MethodTriggerRemove  = "trigger.remove"
	MethodDaemonStatus   = "daemon.status"
	MethodDaemonStop     = "daemon.stop"
	MethodDaemonRestart  = "daemon.restart"
	MethodConfigGet      = "config.get"
	MethodConfigSet      = "config.set"
	MethodSkillList      = "skill.list"
	MethodSkillValidate  = "skill.validate"
	MethodSessionNew     = "session.new"
	MethodSessionRestore = "session.restore"
	MethodCompact        = "session.compact"
	MethodUsage          = "session.usage"
)

const (
	ConfigActionUpsertModel   = "upsert_model"
	ConfigActionDeleteModel   = "delete_model"
	ConfigActionActivateModel = "activate_model"
	ConfigActionUpdateGeneral = "update_general"
)

const (
	NotifyStream              = "agent.stream"
	NotifyReasoning           = "agent.reasoning"
	NotifyToolStart           = "agent.tool_start"
	NotifyToolEnd             = "agent.tool_end"
	NotifyAskUser             = "agent.ask_user"
	NotifyGuardConfirm        = "agent.guard_confirm"
	NotifyDaemonState         = "daemon.state"
	NotifyPerception          = "perception.event"
	NotifyMemoryUpdated       = "memory.updated"
	NotifyCompactResult       = "session.compact_result"
	NotifyMemoryListResult    = "memory.list_result"
	NotifySessionRestoreMsg   = "session.restore_message"
	NotifySessionRestoreInput = "session.restore_input"
)
