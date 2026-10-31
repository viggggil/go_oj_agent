<template>
  <section class="workspace">
    <div class="page-head">
      <div>
        <h1>{{ editing ? '管理题目' : '新建题目' }}</h1>
        <p v-if="editing">题目 {{ id }} · {{ archived ? '已归档' : '正常' }}</p>
      </div>
      <button v-if="editing && !archived" class="danger" @click="archiveProblem">归档题目</button>
    </div>
    <form class="editor" :class="{ disabled: archived }" @submit.prevent="save">
      <label
        >题目名称<input v-model="form.title" :disabled="archived" required maxlength="255" /></label
      ><label
        >Slug<input
          v-model="form.slug"
          :disabled="archived"
          required
          maxlength="255"
          pattern="[a-z0-9]+(?:-[a-z0-9]+)*" /></label
      ><label class="wide"
        >题面描述<textarea
          v-model="form.description"
          :disabled="archived"
          required
        ></textarea></label
      ><label class="wide"
        >数据范围<textarea
          v-model="form.constraints"
          :disabled="archived"
          maxlength="10000"
          required
          placeholder="例如：\n- 1 ≤ n ≤ 2×10⁵\n- |aᵢ| ≤ 10⁹"
        ></textarea>
      </label>
      <div class="wide problem-samples-editor">
        <div class="section-head"><h2>样例（最多 3 组）</h2></div>
        <div v-for="(sample, index) in form.samples" :key="index" class="sample-editor-row">
          <h3>样例 {{ index + 1 }}</h3>
          <label
            >输入<textarea
              v-model="sample.input"
              :disabled="archived"
              maxlength="65536"
              required
            ></textarea>
          </label>
          <label
            >输出<textarea
              v-model="sample.output"
              :disabled="archived"
              maxlength="65536"
              required
            ></textarea>
          </label>
          <button v-if="!archived" type="button" class="danger-text" @click="removeSample(index)">
            删除
          </button>
        </div>
        <button
          v-if="!archived && form.samples.length < 3"
          type="button"
          class="button secondary"
          @click="addSample"
        >
          添加样例
        </button>
      </div>
      <label
        >难度<select v-model.number="form.difficulty" :disabled="archived">
          <option :value="1">简单</option>
          <option :value="2">中等</option>
          <option :value="3">困难</option>
        </select></label
      ><label
        >标签<input
          v-model="tags"
          :disabled="archived"
          maxlength="1318"
          placeholder="array, math" /></label
      ><label
        >时间限制 (ms)<input
          v-model.number="form.time_limit_ms"
          :disabled="archived"
          type="number"
          min="1"
          max="600000"
          required /></label
      ><label
        >内存限制 (KB)<input
          v-model.number="form.memory_limit_kb"
          :disabled="archived"
          type="number"
          min="1"
          max="1048576"
          required
      /></label>
      <div class="wide actions">
        <button :disabled="saving || archived">
          {{ archived ? '题目已归档' : saving ? '保存中' : '保存题目' }}
        </button>
      </div>
    </form>
    <p v-if="message" :class="messageIsError ? 'error' : 'notice'">{{ message }}</p>
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
      <form v-if="!archived" class="upload" @submit.prevent="upload">
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
        ><button
          v-if="!archived && item.status !== 2"
          class="danger-text"
          @click="archiveCase(item.id)"
        >
          归档
        </button>
      </div>
    </section>
  </section>
</template>
<script setup lang="ts">
  import { onMounted, reactive, ref } from 'vue'
  import { useRoute, useRouter } from 'vue-router'
  import { apiErrorMessage, problemApi } from '../api'
  import type { ProblemInput, Testcase } from '../types'
  const route = useRoute(),
    router = useRouter(),
    id = Number(route.params.id || 0),
    editing = Boolean(id),
    saving = ref(false),
    message = ref(''),
    messageIsError = ref(false),
    archived = ref(false),
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
    constraints: '',
    samples: [{ input: '', output: '' }],
  })
  async function save() {
    if (archived.value) return
    saving.value = true
    message.value = ''
    messageIsError.value = false
    form.title = form.title.trim()
    form.slug = form.slug.trim()
    const normalizedTags = [
      ...new Set(
        tags.value
          .split(',')
          .map((v) => v.trim().toLowerCase())
          .filter(Boolean),
      ),
    ]
    if (!form.title || !form.description.trim()) return failValidation('题目名称和题面描述不能为空')
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(form.slug) || form.slug.length > 255)
      return failValidation('Slug 只能包含小写字母、数字和单个连字符，且不能超过 255 字符')
    if (normalizedTags.length > 20) return failValidation('标签最多 20 个')
    if (normalizedTags.some((tag) => tag.length > 64))
      return failValidation('单个标签不能超过 64 字符')
    form.tags = normalizedTags
    const byteLength = (value: string) => new TextEncoder().encode(value).length
    if (!form.constraints.trim()) return failValidation('请填写数据范围')
    if (form.samples.length < 1 || form.samples.length > 3)
      return failValidation('请填写 1 至 3 组样例')
    if (byteLength(form.constraints) > 10000) return failValidation('数据范围不能超过 10,000 字节')
    if (
      form.samples.some(
        (sample) => byteLength(sample.input) > 65536 || byteLength(sample.output) > 65536,
      )
    )
      return failValidation('每组样例的输入和输出分别不能超过 65,536 字节')
    try {
      const data = editing
        ? (await problemApi.update(id, form)).data
        : (await problemApi.create(form)).data
      message.value = '已保存'
      if (!editing) router.replace(`/problems/${data.problem.id}/edit`)
    } catch (error) {
      messageIsError.value = true
      message.value = apiErrorMessage(error, '保存题目')
    } finally {
      saving.value = false
    }
  }
  function addSample() {
    if (form.samples.length < 3) form.samples.push({ input: '', output: '' })
  }
  function removeSample(index: number) {
    form.samples.splice(index, 1)
  }
  function failValidation(text: string) {
    messageIsError.value = true
    message.value = text
    saving.value = false
  }
  async function loadTestcases() {
    try {
      testcases.value = (await problemApi.testcases(id, includeArchived.value)).data.items || []
    } catch (error) {
      messageIsError.value = true
      message.value = apiErrorMessage(error, '加载测试用例')
    }
  }
  async function upload() {
    const input = inputPicker.value?.files?.[0],
      output = outputPicker.value?.files?.[0]
    if (!input || !output) return
    const expectedIn = `${caseNo.value}.in`,
      expectedOut = `${caseNo.value}.out`
    if (input.name !== expectedIn || output.name !== expectedOut) {
      messageIsError.value = true
      message.value = `文件必须命名为 ${expectedIn} 和 ${expectedOut}`
      return
    }
    try {
      await problemApi.uploadTestcase(id, caseNo.value, input, output)
      messageIsError.value = false
      message.value = '测试用例已上传'
      await loadTestcases()
    } catch (error) {
      messageIsError.value = true
      message.value = apiErrorMessage(error, '上传测试用例')
    }
  }
  async function archiveCase(testcaseId: number) {
    if (archived.value) return
    try {
      await problemApi.archiveTestcase(id, testcaseId)
      await loadTestcases()
    } catch (error) {
      messageIsError.value = true
      message.value = apiErrorMessage(error, '归档测试用例')
    }
  }
  async function archiveProblem() {
    if (!confirm('确认归档这道题？')) return
    try {
      await problemApi.archive(id)
      archived.value = true
      messageIsError.value = false
      message.value = '题目已归档'
    } catch (error) {
      messageIsError.value = true
      message.value = apiErrorMessage(error, '归档题目')
    }
  }
  onMounted(async () => {
    if (!editing) return
    try {
      const { data } = await problemApi.get(id)
      archived.value = data.problem.status === 2
      Object.assign(form, {
        title: data.problem.title,
        slug: data.problem.slug,
        description: data.problem.description,
        difficulty: data.problem.difficulty,
        time_limit_ms: data.problem.time_limit_ms,
        memory_limit_kb: data.problem.memory_limit_kb,
        constraints: data.problem.constraints || '',
        samples: (data.problem.samples || []).map((sample) => ({
          input: sample.input,
          output: sample.output,
        })),
      })
      tags.value = data.problem.tags.map((t) => t.name).join(', ')
      await loadTestcases()
    } catch (error) {
      messageIsError.value = true
      message.value = apiErrorMessage(error, '加载题目')
    }
  })
</script>
