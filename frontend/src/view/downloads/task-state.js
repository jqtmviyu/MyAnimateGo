export const statusNames = ['已暂停', '等待校验', '校验中', '等待下载', '下载中', '等待做种', '做种中']
export function trackerState(tracker) {
  if (tracker.announceState === 3) return '通告更新中'
  if (tracker.announceState === 2) return '等待通告'
  if (!tracker.hasAnnounced) return '尚未通告'
  return tracker.lastAnnounceSucceeded || tracker.lastAnnounceResult === 'Success' ? '正常' : tracker.lastAnnounceResult || '通告失败'
}
export function taskError(task) {
  let torrentError = task.errorString || '', trackerError = '', anyWorking = false
  for (const tracker of task.trackerStats || []) {
    if (tracker.isBackup) continue
    const error = tracker.hasAnnounced && !tracker.lastAnnounceSucceeded ? tracker.lastAnnounceResult || '' : ''
    if (!error || error === 'Success') anyWorking = true
    else if (!trackerError) {
      if (error === torrentError) torrentError = ''
      trackerError = `Tracker：${error}`
    }
  }
  return anyWorking || task.status === 0 ? torrentError : trackerError || torrentError
}
export const taskStatus = task => taskError(task) ? '异常' : statusNames[task.status] || '未知'
export const taskIdentity = task => `${task.hashString || task.id}:${task.name}:${task.editDate ?? ''}:${task['file-count'] ?? ''}`
export function shortcutAllowed({active, modalOpen, popupDetails, target, document}) {
  return active && !modalOpen && !popupDetails && !target?.closest?.('input,textarea,select,[contenteditable=true],[role=combobox],[role=menu],[role=dialog],.torrent-details') && !document?.querySelector?.('[role=dialog][data-state=open],[role=alertdialog][data-state=open],[role=menu][data-state=open]')
}
