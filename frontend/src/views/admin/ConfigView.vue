<template>
  <div>
    <div class="page-title">AI 配置</div>
    <div class="page-desc">
      运行时配置（SQLite 持久化，键注册表由后端声明）。
      <el-tag type="success" size="small">立即生效</el-tag>
      <el-tag type="warning" size="small">重启机器人生效</el-tag>
      <el-tag type="info" size="small">重启进程生效</el-tag>
    </div>

    <div v-for="g in groups" :key="g.name" class="card">
      <div class="card-title">{{ g.name }}</div>
      <el-form label-position="left">
        <div v-for="item in g.items" :key="item.key" class="form-row">
          <label>
            {{ item.label }}
            <el-tag :type="scopeTag(item.scope).type" size="small" style="margin-left:6px">
              {{ scopeTag(item.scope).text }}
            </el-tag>
          </label>
          <el-switch
            v-if="item.type === 'bool'"
            v-model="form[item.key]"
            active-value="true"
            inactive-value="false"
          />
          <el-input-number
            v-else-if="item.type === 'number'"
            v-model="form[item.key]"
            :controls="false"
            style="width:200px"
          />
          <el-input
            v-else
            v-model="form[item.key]"
            style="max-width:280px"
            :placeholder="item.secret ? '留空则不修改' : ''"
            :show-password="item.secret"
            @focus="onSecretFocus(item)"
          />
        </div>
      </el-form>
    </div>

    <div style="margin-top:4px">
      <el-button type="primary" size="small" :loading="saving" @click="save">保存配置</el-button>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { adminReq } from '../../api/admin'

// 敏感值掩码（与后端 config.SecretMask 一致）：GET 回包用其代替明文，PUT 原样传回=不修改
const SECRET_MASK = '********'

const entries = ref([])
const form = reactive({})
const saving = ref(false)

const SCOPE_TAGS = {
  hot: { type: 'success', text: '立即生效' },
  'bot-restart': { type: 'warning', text: '重启机器人生效' },
  'process-restart': { type: 'info', text: '重启进程生效' },
}

function scopeTag(scope) {
  return SCOPE_TAGS[scope] || { type: 'info', text: scope }
}

// 按注册表顺序分组（组顺序 = 组内首个键出现的顺序）
const groups = computed(() => {
  const out = []
  const byName = {}
  for (const e of entries.value) {
    const name = e.group || '其他'
    if (!byName[name]) {
      byName[name] = { name, items: [] }
      out.push(byName[name])
    }
    byName[name].items.push(e)
  }
  return out
})

function initForm() {
  for (const e of entries.value) {
    if (e.type === 'number') {
      const n = Number(e.value)
      form[e.key] = e.value === '' || Number.isNaN(n) ? 0 : n
    } else if (e.type === 'bool') {
      form[e.key] = e.value === 'true' || e.value === '1' ? 'true' : 'false'
    } else {
      form[e.key] = e.value ?? ''
    }
  }
}

// 掩码值获得焦点时清空，便于直接输入新值；留空则保存时不提交该键
function onSecretFocus(item) {
  if (item.secret && form[item.key] === SECRET_MASK) {
    form[item.key] = ''
  }
}

async function load() {
  entries.value = await adminReq('/api/admin/config')
  initForm()
}

async function save() {
  const updates = {}
  for (const e of entries.value) {
    let v = form[e.key]
    if (e.type === 'number') v = String(v ?? 0)
    if (e.type === 'bool') v = v ? 'true' : 'false'
    // 敏感键：留空或掩码原样 = 不修改，不提交
    if (e.secret && (v === '' || v === SECRET_MASK)) continue
    // 未改动的键不提交
    if (String(v) === String(e.value ?? '')) continue
    updates[e.key] = v
  }
  if (Object.keys(updates).length === 0) {
    ElMessage.info('没有改动')
    return
  }
  saving.value = true
  try {
    await adminReq('/api/admin/config', { method: 'PUT', body: JSON.stringify(updates) })
    ElMessage.success('配置已保存')
    await load()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.form-row { display: flex; align-items: center; gap: 12px; padding: 10px 0; border-bottom: 1px solid #f0f1f3; }
.form-row:last-child { border-bottom: none; }
.form-row label { width: 320px; flex: none; font-size: 13.5px; }
</style>
