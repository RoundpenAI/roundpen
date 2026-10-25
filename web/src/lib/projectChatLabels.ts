const ROLE_LABEL = {
  implementer: '实现',
  reviewer: '审查',
  tester: '测试',
} as const

export function roleLabel(role: string | undefined): string {
  if (role === 'implementer' || role === 'reviewer' || role === 'tester') {
    return ROLE_LABEL[role]
  }
  return 'Worker'
}
