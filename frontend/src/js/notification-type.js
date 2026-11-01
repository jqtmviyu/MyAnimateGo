export const notificationTypeList = [
  {name: 'WEB_HOOK', label: 'Webhook'},
  {name: 'EMBY_REFRESH', label: 'Emby 媒体库刷新'},
]

export const getLabel = name => notificationTypeList.find(item => item.name === name)?.label || name
