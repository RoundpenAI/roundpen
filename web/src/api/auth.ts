import { api } from './client'
import type { User } from './client'

export type ProfileUpdate = {
  fullname?: string
  orgName?: string
  about?: string
  timezone?: string
}

export const auth = {
  me: () => api<User>('/v1/auth/user'),
  updateProfile: (body: ProfileUpdate) =>
    api<User>('/v1/auth/profile', {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  login: (user: string, password: string) =>
    api<{ user: User }>('/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ user, password }),
    }),
  bootstrapHint: () =>
    api<{ username?: string; password?: string }>('/v1/auth/bootstrap-hint'),
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
