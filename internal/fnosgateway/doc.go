// Package fnosgateway bridges the fnOS (飞牛) unified-gateway identity into
// Roundpen's own auth stack.
//
// fnOS injects X-Trim-Userid / X-Trim-Isadmin / X-Trim-Username on requests it
// forwards to the app's Unix socket; there is no OIDC/JWT. The gateway maps
// that injected uid onto a per-user Roundpen account (fnos-<uid>), mints a
// normal session, and hands the request off to the existing auth middleware, so
// WebSocket, /v1/* and the bridge stay untouched.
//
// The bridge is only meaningful on fnOS: identity.go and listener.go carry a
// fnos build tag, so a default build compiles this file alone.
package fnosgateway
