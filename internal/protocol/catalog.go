package protocol

// RuntimeCatalog 描述第三方客户端可以发现并使用的公开 Runtime 能力。
// Methods 与 Notifications 使用真实协议名称；Features 只补充无法由名称直接推断的细粒度语义。
type RuntimeCatalog struct {
	Methods       []string `json:"methods"`
	Notifications []string `json:"notifications"`
	Features      []string `json:"features"`
}

const (
	FeatureRunSteeringText     = "agent.steer.text"
	FeatureModelAuthModeBearer = "config.model.auth_mode.bearer"
	FeatureModelAuthModeBoth   = "config.model.auth_mode.both"
	FeatureProjectSkills       = "skill.project"
	FeatureSessionHandoff      = "session.handoff"
)

var runtimeCatalogMethods = []string{
	MethodAskReply,
	MethodCancel,
	MethodGuardReply,
	MethodResumeRun,
	MethodSendMessage,
	MethodSteer,
	MethodSteerRemove,
	MethodConfigGet,
	MethodConfigSet,
	MethodMCPList,
	MethodMCPReload,
	MethodMCPToggle,
	MethodMemoryClear,
	MethodMemoryDelete,
	MethodMemoryList,
	MethodRuntimeHello,
	MethodSessionAttach,
	MethodCompact,
	MethodSessionCreate,
	MethodSessionDelete,
	MethodSessionDetach,
	MethodSessionList,
	MethodSessionUpdate,
	MethodUsage,
	MethodSkillList,
	MethodSkillSet,
}

var runtimeCatalogNotifications = []string{
	NotifyAskUser,
	NotifyAgentDelta,
	NotifyGuardConfirm,
	NotifyInteractionResolved,
	NotifyAgentRun,
	NotifySteering,
	NotifyToolEnd,
	NotifyToolGuard,
	NotifyToolStart,
	NotifyUsage,
	NotifyConfigState,
	NotifyMCPUpdated,
	NotifyMemoryState,
	NotifyCompactResult,
	NotifySessionUpdated,
	NotifySessionUserMessage,
	NotifySkillLoad,
	NotifySkillReview,
}

var runtimeCatalogFeatures = []string{
	FeatureRunSteeringText,
	FeatureModelAuthModeBearer,
	FeatureModelAuthModeBoth,
	FeatureSessionHandoff,
	FeatureProjectSkills,
}

// CurrentRuntimeCatalog 返回独立副本，避免调用方修改进程级静态清单。
func CurrentRuntimeCatalog() RuntimeCatalog {
	return RuntimeCatalog{
		Methods:       append([]string(nil), runtimeCatalogMethods...),
		Notifications: append([]string(nil), runtimeCatalogNotifications...),
		Features:      append([]string(nil), runtimeCatalogFeatures...),
	}
}
