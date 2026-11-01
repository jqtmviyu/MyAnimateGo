// 文件顺序固定时，用前缀计数让目录选中判断保持常数时间。
export function selectedPrefix(leaves, selected) {
 const chosen=new Set(selected),prefix=new Uint32Array(leaves.length+1)
 for(let index=0;index<leaves.length;index++)prefix[index+1]=prefix[index]+Number(chosen.has(leaves[index]))
 return prefix
}
export const selectedCount=(prefix,node)=>prefix[node.end]-prefix[node.start]
