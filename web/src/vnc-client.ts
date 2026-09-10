import RFB from './lib/novnc-rfb.ts'

const params = new URLSearchParams(location.search)
const wsUrl = params.get('url')
const status = document.getElementById('status')
const screen = document.getElementById('screen')

if (!status || !screen) {
  throw new Error('vnc page missing #status or #screen')
}

if (!wsUrl) {
  status.textContent = 'Missing ?url= websocket'
} else {
  try {
    const rfb = new RFB(screen, wsUrl, {
      wsProtocols: ['binary'],
    })
    status.dataset.rfb = 'ready'
    rfb.scaleViewport = true
    rfb.resizeSession = true
    rfb.addEventListener('connect', () => {
      status.textContent = 'Connected'
    })
    rfb.addEventListener('disconnect', (e) => {
      const detail = (e as Event & { detail?: { clean?: boolean } }).detail
      status.textContent = detail?.clean ? 'Disconnected' : 'Connection lost'
    })
    rfb.addEventListener('securityfailure', (e) => {
      const detail = (e as Event & { detail?: { status?: string } }).detail
      status.textContent = 'Security failure: ' + (detail?.status || '')
    })
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err)
    status.dataset.rfb = 'failed'
    status.textContent = 'Failed to load VNC client: ' + message
  }
}
