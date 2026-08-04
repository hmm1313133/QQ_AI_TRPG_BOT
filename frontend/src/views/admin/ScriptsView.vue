<template>
  <div>
    <div class="page-title">剧本管理</div>
    <div class="page-desc">上传、查看与删除剧本</div>

    <div class="card">
      <div class="card-title">上传剧本（PDF / Word / 文本，AI 自动识别）</div>
      <div style="display:flex;gap:10px;align-items:center">
        <input ref="fileEl" type="file" class="file-input" accept=".pdf,.docx,.txt,.md">
        <el-button type="primary" size="small" @click="upload">上传并分析</el-button>
      </div>
      <div v-if="uploading" class="progress-bar"><div :style="{ width: '40%' }"></div></div>
      <div class="muted" style="margin-top:8px">{{ uploadStatus }}</div>
    </div>

    <div class="card">
      <div class="card-title">剧本列表</div>
      <div style="margin-bottom:12px">
        <el-button type="primary" plain size="small" @click="openCreate">手动创建</el-button>
      </div>
      <el-table :data="scripts" empty-text="暂无剧本">
        <el-table-column prop="name" label="名称" />
        <el-table-column prop="title" label="标题" />
        <el-table-column prop="system" label="规则" />
        <el-table-column prop="nodes" label="节点" width="80" />
        <el-table-column prop="characters" label="角色" width="80" />
        <el-table-column label="操作" width="260">
          <template #default="{ row }">
            <el-button size="small" @click="openEdit(row.id)">编辑</el-button>
            <el-button size="small" plain @click="collectAssets(row)">收藏到素材库</el-button>
            <el-button type="danger" plain size="small" @click="remove(row.id)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <ScriptEditor v-model:visible="editorVisible" :script-id="editingId" @saved="loadScripts" />
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adminApi, getAdminToken, scriptApi } from '../../api/admin'
import ScriptEditor from '../../components/ScriptEditor.vue'

const scripts = ref([])
const fileEl = ref(null)
const uploading = ref(false)
const uploadStatus = ref('')
const editorVisible = ref(false)
const editingId = ref('')

async function loadScripts() {
  scripts.value = (await adminApi('/api/admin/scripts')) || []
}

function openCreate() {
  editingId.value = ''
  editorVisible.value = true
}

function openEdit(id) {
  editingId.value = id
  editorVisible.value = true
}

async function remove(id) {
  try {
    await ElMessageBox.confirm('确认删除该剧本？', '提示', { type: 'warning' })
  } catch {
    return
  }
  await fetch(`/api/admin/scripts/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: { Authorization: 'Bearer ' + getAdminToken() },
  })
  ElMessage.success('已删除')
  loadScripts()
}

async function collectAssets(row) {
  try {
    await ElMessageBox.confirm(
      `将把剧本「${row.name}」的背景、角色、场景、组织与主线派生素材写入素材库并打上标签，重复操作会自动跳过已入库素材。确定继续？`,
      '收藏到素材库',
      { type: 'info', confirmButtonText: '收藏', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    const res = await scriptApi.collectAssets(row.id)
    const errs = res?.errors || []
    if (errs.length) {
      ElMessage.warning(`已入库 ${res?.created || 0} 条，跳过 ${res?.skipped || 0} 条，失败 ${errs.length} 条：${errs.join('；')}`)
    } else {
      ElMessage.success(`已入库 ${res?.created || 0} 条，跳过 ${res?.skipped || 0} 条`)
    }
  } catch (e) {
    ElMessage.error('收藏失败：' + e.message)
  }
}

async function upload() {
  const file = fileEl.value?.files[0]
  if (!file) { ElMessage.warning('请先选择文件'); return }
  const fd = new FormData()
  fd.append('file', file)
  uploading.value = true
  uploadStatus.value = '上传中…'
  const resp = await fetch('/api/admin/scripts/upload', {
    method: 'POST',
    headers: { Authorization: 'Bearer ' + getAdminToken() },
    body: fd,
  })
  const data = await resp.json()
  if (!data.task_id) {
    uploading.value = false
    uploadStatus.value = '上传失败'
    return
  }
  pollTask(data.task_id)
}

async function pollTask(taskID) {
  const t = await adminApi('/api/admin/tasks/' + taskID)
  uploadStatus.value = `[${t.stage}] ${t.message || ''}`
  if (t.done) {
    uploading.value = false
    if (t.error) {
      ElMessage.error('分析失败')
      uploadStatus.value = t.error
    } else {
      ElMessage.success('分析完成')
    }
    loadScripts()
    return
  }
  setTimeout(() => pollTask(taskID), 2000)
}

onMounted(loadScripts)
</script>

<style scoped>
.file-input {
  padding: 8px 12px; font: inherit; font-size: 13.5px;
  border: 1px solid var(--border); border-radius: 8px; background: var(--surface);
}
.progress-bar { height: 6px; background: #eef0f3; border-radius: 3px; overflow: hidden; margin-top: 8px; }
.progress-bar > div { height: 100%; background: var(--primary); border-radius: 3px; transition: width .4s; }
</style>
