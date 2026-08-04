<template>
  <div>
    <div class="page-title">角色卡</div>
    <div class="page-desc">玩家与 NPC 角色卡管理</div>

    <div class="card">
      <div class="toolbar">
        <el-input v-model="keyword" placeholder="按名称 / 玩家搜索" clearable style="max-width:260px" />
        <el-button type="primary" @click="openCreate">新建角色卡</el-button>
      </div>
      <el-table :data="filtered" empty-text="暂无角色卡">
        <el-table-column prop="name" label="名称" width="140" />
        <el-table-column prop="player" label="玩家" width="160">
          <template #default="{ row }"><span class="mono">{{ row.player || '-' }}</span></template>
        </el-table-column>
        <el-table-column prop="system" label="规则" width="90" />
        <el-table-column label="属性">
          <template #default="{ row }"><span class="mono">{{ attrsText(row) }}</span></template>
        </el-table-column>
        <el-table-column label="状态">
          <template #default="{ row }"><span class="mono">{{ statusText(row) }}</span></template>
        </el-table-column>
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openEdit(row)">编辑</el-button>
            <el-button type="danger" plain size="small" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 新建 -->
    <el-dialog v-model="createVisible" title="新建角色卡" width="560px">
      <el-form label-width="90px">
        <el-form-item label="名称" required>
          <el-input v-model="createForm.name" placeholder="角色名" />
        </el-form-item>
        <el-form-item label="玩家">
          <el-input v-model="createForm.player" placeholder="QQ openid；NPC 卡填 npc:{剧本ID}" />
        </el-form-item>
        <el-form-item label="规则集" required>
          <el-select v-model="createForm.system" style="width:180px">
            <el-option label="coc7" value="coc7" />
            <el-option label="dnd5e" value="dnd5e" />
          </el-select>
        </el-form-item>
        <el-form-item label="背景故事">
          <el-input v-model="createForm.backstory" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="属性 attrs">
          <KVEditor v-model="createForm.attrs" />
        </el-form-item>
        <el-form-item label="技能 skills">
          <KVEditor v-model="createForm.skills" />
        </el-form-item>
        <el-form-item label="状态 status">
          <KVEditor v-model="createForm.status" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="create">创建</el-button>
      </template>
    </el-dialog>

    <!-- 编辑 -->
    <el-drawer v-model="editVisible" :title="`编辑角色卡：${editForm.name}`" size="480px">
      <el-form label-width="90px">
        <el-form-item label="名称">
          <el-input v-model="editForm.name" />
        </el-form-item>
        <el-form-item label="规则集">
          <el-select v-model="editForm.system" style="width:180px">
            <el-option label="coc7" value="coc7" />
            <el-option label="dnd5e" value="dnd5e" />
          </el-select>
        </el-form-item>
        <el-form-item label="背景故事">
          <el-input v-model="editForm.backstory" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="属性 attrs">
          <KVEditor v-model="editForm.attrs" />
        </el-form-item>
        <el-form-item label="技能 skills">
          <KVEditor v-model="editForm.skills" />
        </el-form-item>
        <el-form-item label="状态 status">
          <KVEditor v-model="editForm.status" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="update">保存</el-button>
      </template>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { adminReq } from '../../api/admin'
import KVEditor from '../../components/KVEditor.vue'

const chars = ref([])
const keyword = ref('')
const saving = ref(false)

const createVisible = ref(false)
const createForm = reactive({ name: '', player: '', system: 'coc7', backstory: '', attrs: {}, skills: {}, status: {} })

const editVisible = ref(false)
const editForm = reactive({ id: '', name: '', system: '', backstory: '', attrs: {}, skills: {}, status: {} })

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return chars.value
  return chars.value.filter(
    (c) => (c.name || '').toLowerCase().includes(kw) || (c.player || '').toLowerCase().includes(kw)
  )
})

function attrsText(c) {
  return Object.entries(c.attrs || {}).slice(0, 6).map(([k, v]) => `${k}=${v}`).join(' ')
}
function statusText(c) {
  return Object.entries(c.status || {}).map(([k, v]) => `${k}=${v}`).join(' ')
}

async function load() {
  chars.value = (await adminReq('/api/admin/characters')) || []
}

function openCreate() {
  Object.assign(createForm, { name: '', player: '', system: 'coc7', backstory: '', attrs: {}, skills: {}, status: {} })
  createVisible.value = true
}

async function create() {
  if (!createForm.name.trim()) { ElMessage.warning('名称不能为空'); return }
  saving.value = true
  try {
    await adminReq('/api/admin/characters', {
      method: 'POST',
      body: JSON.stringify({
        name: createForm.name.trim(),
        player: createForm.player.trim(),
        system: createForm.system,
        backstory: createForm.backstory,
        attrs: createForm.attrs,
        skills: createForm.skills,
        status: createForm.status,
      }),
    })
    ElMessage.success('已创建')
    createVisible.value = false
    load()
  } catch (e) {
    ElMessage.error(e.status === 409 ? '同名角色卡已存在' : e.message)
  } finally {
    saving.value = false
  }
}

function openEdit(row) {
  Object.assign(editForm, {
    id: row.id,
    name: row.name,
    system: row.system,
    backstory: row.backstory || '',
    attrs: { ...(row.attrs || {}) },
    skills: { ...(row.skills || {}) },
    status: { ...(row.status || {}) },
  })
  editVisible.value = true
}

async function update() {
  saving.value = true
  try {
    await adminReq(`/api/admin/characters/${encodeURIComponent(editForm.id)}`, {
      method: 'PUT',
      body: JSON.stringify({
        name: editForm.name,
        system: editForm.system,
        backstory: editForm.backstory,
        attrs: editForm.attrs,
        skills: editForm.skills,
        status: editForm.status,
      }),
    })
    ElMessage.success('已保存')
    editVisible.value = false
    load()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(`确认删除角色卡「${row.name}」？`, '提示', { type: 'warning' })
  } catch {
    return
  }
  try {
    await adminReq(`/api/admin/characters/${encodeURIComponent(row.id)}`, { method: 'DELETE' })
    ElMessage.success('已删除')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

onMounted(load)
</script>

<style scoped>
.toolbar { display: flex; gap: 10px; margin-bottom: 14px; }
</style>
