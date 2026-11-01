import {reactive} from 'vue'

export const runtimeState = reactive({username: '我的账号', developmentMode: false, automaticTasksPaused: undefined})

export function updateRuntimeState(config = {}) {
  if (typeof config.developmentMode === 'boolean') runtimeState.developmentMode = config.developmentMode
  if (config.login?.username) runtimeState.username = config.login.username
  if (typeof config.automaticTasksPaused === 'boolean') runtimeState.automaticTasksPaused = config.automaticTasksPaused
}
