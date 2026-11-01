export function sameDetailValue(before,after) {
 if(before===after)return true
 if(!before||!after||typeof before!=='object'||typeof after!=='object'||Array.isArray(before)!==Array.isArray(after))return false
 const keys=Object.keys(before)
 return keys.length===Object.keys(after).length&&keys.every(key=>Object.hasOwn(after,key)&&sameDetailValue(before[key],after[key]))
}
// 文件树按路径和大小建树；完成字节数由 fileStats 独立更新。
function sameFileMetadata(before,after) {
 return Array.isArray(before)&&Array.isArray(after)&&before.length===after.length&&before.every((file,index)=>file.name===after[index].name&&file.length===after[index].length)
}
export function mergeDetailTask(before,after) {
 let changed=false
 const next={...before}
 for(const [key,value] of Object.entries(after)){
  const equal=key==='files'?sameFileMetadata(before[key],value):sameDetailValue(before[key],value)
  if(!equal){next[key]=value;changed=true}
 }
 return changed?next:before
}
