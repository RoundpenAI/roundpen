package settingitems

// Slot keys of the built-in usage sites. The keys are a shared vocabulary:
// consumers register their SlotDef with one of them, the migration seeds
// bindings for them, and the UI renders them.
const (
	SlotProxyAgent   = "proxy.agent"
	SlotProxyBrowser = "proxy.browser"

	SlotLLMDefault    = "llm.default"
	SlotLLMAgent      = "llm.agent"
	SlotLLMSysAgent   = "llm.sysagent"
	SlotLLMClassifier = "llm.classifier"
	SlotLLMPlanner    = "llm.planner"
	SlotLLMEmbedding  = "llm.embedding"
	SlotLLMPlan       = "llm.plan"
	SlotLLMVision     = "llm.vision"
	SlotLLMCoding     = "llm.coding"

	SlotSearchDefault  = "search.default"
	SlotBrowserDefault = "browser.default"
)
