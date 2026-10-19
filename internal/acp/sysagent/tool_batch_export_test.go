package sysagent

// ToolParallelSafeForTest exposes toolParallelSafe for unit tests.
func ToolParallelSafeForTest(name string, mutating bool) bool {
	return toolParallelSafe(name, mutating)
}
