// 设定条目分类（设计文档《世界设定库与按需加载设计.md》§3.1，八种）
export const LORE_CATEGORIES = [
  { value: 'background', label: '背景' },
  { value: 'geo', label: '地理' },
  { value: 'faction', label: '势力' },
  { value: 'character', label: '人物' },
  { value: 'item', label: '物品' },
  { value: 'rule', label: '规则' },
  { value: 'history', label: '历史' },
  { value: 'style', label: '风格' },
]

export function categoryLabel(v) {
  const c = LORE_CATEGORIES.find((x) => x.value === v)
  return c ? c.label : v || '背景'
}

export function emptyLoreEntry() {
  return {
    id: '',
    title: '',
    category: 'background',
    keys: [],
    secondary_keys: [],
    secondary_mode: 'and_any',
    constant: false,
    position: 'front',
    priority: 50,
    enabled: true,
    content: '',
    source: 'manual',
  }
}
