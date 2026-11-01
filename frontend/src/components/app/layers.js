export const layerContext = Symbol('dialog-layer')
export const dialogDepthContext = Symbol('dialog-depth')
let level = 900

export function nextDialogLayer() {
  level += 100
  return level
}
