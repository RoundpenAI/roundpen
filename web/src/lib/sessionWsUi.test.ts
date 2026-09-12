import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  nextWsUiStatus,
  wsCanSendProp,
  wsCloseDetail,
  wsConnectTimeoutDetail,
  wsInputPlaceholder,
  wsReconnectDelayMs,
  wsStatusLabel,
  WS_CONNECT_TIMEOUT_MS,
  type WsUiStatus,
} from './sessionWsUi.ts'

describe('nextWsUiStatus', () => {
  it('Strict Mode remount: cleanup keeps status, effect_start clears to connecting, then open', () => {
    let status: WsUiStatus = 'connecting'
    status = nextWsUiStatus(status, 'socket_open')
    assert.equal(status, 'open')
    status = nextWsUiStatus(status, 'effect_cleanup')
    assert.equal(status, 'open')
    status = nextWsUiStatus(status, 'effect_start')
    assert.equal(status, 'connecting')
    status = nextWsUiStatus(status, 'socket_open')
    assert.equal(status, 'open')
  })

  it('live close schedules reconnect via connecting', () => {
    let status: WsUiStatus = 'open'
    status = nextWsUiStatus(status, 'socket_close')
    assert.equal(status, 'connecting')
  })

  it('timeout and error map to error status', () => {
    assert.equal(nextWsUiStatus('connecting', 'connect_timeout'), 'error')
    assert.equal(nextWsUiStatus('connecting', 'socket_error'), 'error')
  })
})

describe('ws UI copy and canSend', () => {
  it('shows detail in the connecting chip position', () => {
    assert.equal(wsStatusLabel('open'), '已连接')
    assert.equal(wsStatusLabel('connecting'), '连接中')
    assert.equal(wsStatusLabel('connecting', '连接超时（8s）'), '连接超时（8s）')
    assert.equal(wsStatusLabel('error', '连接异常中断'), '连接异常中断')
    assert.equal(wsStatusLabel('error'), '连接失败')
  })

  it('formats timeout and close details', () => {
    assert.equal(WS_CONNECT_TIMEOUT_MS, 8000)
    assert.equal(wsConnectTimeoutDetail(), '连接超时（8s）')
    assert.match(wsCloseDetail(1006, ''), /异常中断/)
    assert.match(wsCloseDetail(1011, 'restart agent: boom'), /restart agent/)
  })

  it('never force-disables send; connecting queues instead', () => {
    assert.equal(wsCanSendProp('connecting'), undefined)
    assert.equal(wsCanSendProp('open'), undefined)
    assert.equal(wsInputPlaceholder('connecting'), '给助手发消息…')
  })

  it('reconnects immediately on first failure, then backs off', () => {
    assert.equal(wsReconnectDelayMs(0), 0)
    assert.equal(wsReconnectDelayMs(1), 1000)
    assert.equal(wsReconnectDelayMs(2), 2000)
    assert.equal(wsReconnectDelayMs(4), 8000)
    assert.equal(wsReconnectDelayMs(10), 8000)
  })
})
