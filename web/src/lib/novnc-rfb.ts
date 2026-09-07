import _RFB from '@novnc/novnc/lib/rfb.js'

export type RFBOptions = {
  wsProtocols?: string[]
}

export type RFBInstance = {
  scaleViewport: boolean
  resizeSession: boolean
  addEventListener: (type: string, listener: (ev: Event) => void) => void
}

type RFBCtor = new (
  target: Element,
  urlOrChannel: string,
  options?: RFBOptions,
) => RFBInstance

// @novnc/novnc ships CJS. Vite/jsDelivr interop sometimes yields the
// namespace object instead of exports.default — then `new RFB` throws
// "RFB is not a constructor".
const RFB = ((_RFB as { default?: RFBCtor }).default ?? _RFB) as RFBCtor

if (typeof RFB !== 'function') {
  throw new Error('RFB is not a constructor')
}

export default RFB
