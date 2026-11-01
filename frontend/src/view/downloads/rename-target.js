// 按实际路径分辨文件和目录，目录里只有一个文件时也保持目录交互。
export function renameTarget(task,requestedPath,requestedName) {
 const first=task.files?.[0]?.name
 if(!first)return null
 const path=requestedPath||first.split('/')[0]
 return {path,name:requestedName||path.split('/').pop(),directory:task.files.some(file=>file.name.startsWith(path+'/'))}
}
