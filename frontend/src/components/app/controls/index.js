import * as basic from './basic'
import * as choices from './choices'
import * as containers from './containers'
import * as surfaces from './surfaces'
import * as table from './table'

export const appControls = {...basic, ...choices, ...containers, ...surfaces, ...table}
export function installControls(app) {
  for (const [name, component] of Object.entries(appControls)) app.component(name, component)
}
