<template>
  <section class="workspace">
    <div class="page-head">
      <div>
        <h1>{{ editing ? '管理题目' : '新建题目' }}</h1>
        <p v-if="editing">题目 {{ id }}</p>
      </div>
      <button v-if="editing" class="danger" @click="archiveProblem">归档题目</button>
    </div>
    <form class="editor" @submit.prevent="save">
      <label>题目名称<input v-model="form.title" required maxlength="255" /></label
      ><label>Slug<input v-model="form.slug" required pattern="[a-z0-9]+(?:-[a-z0-9]+)*" /></label
      ><label class="wide">题面描述<textarea v-model="form.description" required></textarea></label
      ><label
        >难度<select v-model.number="form.difficulty">
          <option :value="1">简单</option>
          <option :value="2">中等</option>
          <option :value="3">困难</option>
        </select></label
      ><label>标签<input v-model="tags" placeholder="array, math" /></label
      ><label
        >时间限制 (ms)<input
          v-model.number="form.time_limit_ms"
          type="number"
          min="1"
          max="600000"
          required /></label
      ><label
        >内存限制 (KB)<input
          v-model.number="form.memory_limit_kb"
          type="number"
          min="1"
          max="1048576"
          required
      /></label>
      <div class="wide actions">
        <button :disabled="saving">{{ saving ? '保存中' : '保存题目' }}</button>
      </div>
    </form>
    <p v-if="message" :class="message.startsWith('失败') ? 'error' : 'notice'">{{ message }}</p>
    <section v-if="editing" class="testcases">
      <div class="section-head">
        <h2>测试用例</h2>
        <label class="toggle"
          ><input
            v-model="includeArchived"
            type="checkbox"
            @change="loadTestcases"
          />显示已归档</label
        >
      </div>
      <form class="upload" @submit.prevent="upload">
        <label>序号<input v-model.number="caseNo" type="number" min="1" required /></label
        ><label
          >{{ caseNo }}.in<input ref="inputPicker" type="file" :accept="'.in'" required /></label
        ><label
          >{{ caseNo }}.out<input ref="outputPicker" type="file" :accept="'.out'" required /></label
        ><button>上传</button>
      </form>
      <div class="testcase-row" v-for="item in testcases" :key="item.id">
        <strong>#{{ item.case_no }}</strong
        ><span>{{ item.input_size_bytes }} / {{ item.output_size_bytes }} bytes</span
        ><span>{{ item.status === 2 ? '已归档' : '有效' }}</span
        ><button v-if="item.status !== 2" class="danger-text" @click="archiveCase(item.id)">
          归档
        </button>
      </div>
    </section>
  </section>
</template>
<script setup lang="ts">
  import { onMounted, reactive, ref } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import { problemApi } from '../api'
  import type { ProblemInput, Testcase } from '../types'
  const route = useRoute(),
    router = useRouter(),
    id = Number(route.params.id || 0),
    editing = Boolean(id),
    saving = ref(false),
    message = ref(''),
    tags = ref(''),
    testcases = ref<Testcase[]>([]),
    includeArchived = ref(false),
    caseNo = ref(1),
    inputPicker = ref<HTMLInputElement>(),
    outputPicker = ref<HTMLInputElement>()
  const form = reactive<ProblemInput>({
    title: '',
    slug: '',
    description: '',
    difficulty: 1,
    time_limit_ms: 1000,
    memory_limit_kb: 65536,
    tags: [],
  })
  async function save() {
    saving.value = true
    message.value = ''
    form.tags = tags.value
      .split(',')
      .map((v) => v.trim().toLowerCase())
      .filter(Boolean)
    try {
      const data = editing
        ? (await problemApi.update(id, form)).data
        : (await problemApi.create(form)).data
      message.value = '已保存'
      if (!editing) router.replace(`/problems/${data.problem.id}/edit`)
    } catch {
      message.value = '失败：无法保存题目'
    } finally {
      saving.value = false
    }
  }
  async function loadTestcases() {
    testcases.value = (await problemApi.testcases(id, includeArchived.value)).data.items || []
  }
  async function upload() {
    const input = inputPicker.value?.files?.[0],
      output = outputPicker.value?.files?.[0]
    if (!input || !output) return
    const expectedIn = `${caseNo.value}.in`,
      expectedOut = `${caseNo.value}.out`
    if (input.name !== expectedIn || output.name !== expectedOut) {
      message.value = `失败：文件必须命名为 ${expectedIn} 和 ${expectedOut}`
      return
    }
    try {
      await problemApi.uploadTestcase(id, caseNo.value, input, output)
      message.value = '测试用例已上传'
      await loadTestcases()
    } catch {
      message.value = '失败：测试用例上传失败'
    }
  }
  async function archiveCase(testcaseId: number) {
    await problemApi.archiveTestcase(id, testcaseId)
    await loadTestcases()
  }
  async function archiveProblem() {
    if (!confirm('确认归档这道题？')) return
    await problemApi.archive(id)
    router.push('/problems')
  }
  onMounted(async () => {
    if (!editing) return
    const { data } = await problemApi.get(id)
    Object.assign(form, {
      title: data.problem.title,
      slug: data.problem.slug,
      description: data.problem.description,
      difficulty: data.problem.difficulty,
      time_limit_ms: data.problem.time_limit_ms,
      memory_limit_kb: data.problem.memory_limit_kb,
    })
    tags.value = data.problem.tags.map((t) => t.name).join(', ')
    await loadTestcases()
  })
</script>
