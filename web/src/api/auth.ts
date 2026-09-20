import { api } from './client'
import type { User } from './client'

export const auth = {
  me: () => api<User>('/v1/auth/user'),
  login: (user: string, password: string) =>
    api<{ user: User }>('/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ user, password }),
    }),
  logout: () => api<void>('/v1/auth/logout', { method: 'POST' }),
  changePassword: (currentPassword: string, newPassword: string) =>
    api<void>('/v1/auth/password', {
      method: 'POST',
      body: JSON.stringify({
        current_password: currentPassword,
        new_password: newPassword,
      }),
    }),
}
