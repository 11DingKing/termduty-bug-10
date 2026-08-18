// Shared formatting helpers used across pages and components so that identifier
// truncation and timestamp rendering stay consistent everywhere.

export function shortId(id: string): string {
  return id.length > 12 ? id.slice(0, 8) + '…' : id
}

export function fmtTime(t: string): string {
  return t ? new Date(t).toLocaleString('zh-CN') : ''
}
