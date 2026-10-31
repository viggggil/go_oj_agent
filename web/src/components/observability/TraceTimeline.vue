<template>
  <section class="trace-timeline">
    <div v-if="runSpan" class="trace-run-summary">
      <article>
        <span>运行耗时</span>
        <strong>{{ runSpan ? `${runSpan.duration_ms.toFixed(2)} ms` : '—' }}</strong>
      </article>
      <article>
        <span>模型调用</span>
        <strong>{{ counts.models }}</strong>
      </article>
      <article>
        <span>工具调用</span>
        <strong>{{ counts.tools }}</strong>
      </article>
      <article>
        <span>知识检索</span>
        <strong>{{ counts.retrievals }}</strong>
      </article>
      <article>
        <span>输入 Token</span>
        <strong>{{ counts.inputTokens || '—' }}</strong>
      </article>
      <article>
        <span>输出 Token</span>
        <strong>{{ counts.outputTokens || '—' }}</strong>
      </article>
      <article>
        <span>运行状态</span>
        <strong>{{ runStatus }}</strong>
      </article>
      <article v-if="errorCode">
        <span>错误码</span>
        <strong class="trace-summary-error">{{ errorCode }}</strong>
      </article>
    </div>
    <div v-else class="trace-run-summary">
      <article>
        <span>Span 数</span>
        <strong>{{ detail.spans.length }}</strong>
      </article>
      <article>
        <span>服务数</span>
        <strong>{{ serviceCount }}</strong>
      </article>
      <article>
        <span>失败阶段</span>
        <strong>{{ failedCount }}</strong>
      </article>
      <article>
        <span>Trace 耗时</span>
        <strong>{{ totalDuration }}</strong>
      </article>
    </div>
    <div class="trace-timeline-heading">
      <h3>{{ runSpan ? '对话执行链路' : '判题执行链路' }}</h3>
      <p>
        {{
          runSpan
            ? '选择阶段可查看该阶段的日志；底层服务调用默认折叠。'
            : `共 ${detail.spans.length} 个判题环节，选择任一环节可查看关联日志。`
        }}
      </p>
    </div>
    <div class="trace-tree-roots">
      <TraceSpanNode
        v-for="node in tree"
        :key="node.span.span_id"
        :span="node.span"
        :children="node.children"
        :selected-span-id="selectedSpanId"
        @select="$emit('select', $event)"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed } from 'vue'
  import type { TraceDetail, TraceSpan } from '../../api'
  import TraceSpanNode, { type TraceSpanTreeNode } from './TraceSpanNode.vue'

  const props = defineProps<{ detail: TraceDetail; selectedSpanId: string }>()
  defineEmits<{ select: [spanId: string] }>()

  const tree = computed(() => {
    const spans =
      props.detail.scope === 'agent'
        ? props.detail.spans.filter((span) => span.name.startsWith('agent.'))
        : props.detail.spans
    if (props.detail.scope !== 'agent') {
      return [...spans]
        .sort((left, right) => Date.parse(left.started_at) - Date.parse(right.started_at))
        .map((span) => ({ span, children: [] }))
    }
    const nodes = new Map<string, TraceSpanTreeNode>()
    for (const span of spans) nodes.set(span.span_id, { span, children: [] })
    const roots: TraceSpanTreeNode[] = []
    for (const node of nodes.values()) {
      const parent = node.span.parent_span_id ? nodes.get(node.span.parent_span_id) : undefined
      if (parent) parent.children.push(node)
      else roots.push(node)
    }
    const sort = (items: TraceSpanTreeNode[]) => {
      items.sort(
        (left, right) => Date.parse(left.span.started_at) - Date.parse(right.span.started_at),
      )
      for (const item of items) sort(item.children)
    }
    sort(roots)
    return roots
  })
  const runSpan = computed(() => props.detail.spans.find((span) => span.name === 'agent.run'))
  const counts = computed(() => {
    const modelSpans = props.detail.spans.filter((span) => span.name === 'agent.model.attempt')
    return {
      models: modelSpans.length,
      tools: props.detail.spans.filter((span) => span.name === 'agent.tool').length,
      retrievals: props.detail.spans.filter((span) => span.name === 'agent.rag.retrieve').length,
      inputTokens: modelSpans.reduce((total, span) => {
        const value = span.attributes['agent.input_tokens']
        return total + (typeof value === 'number' ? value : 0)
      }, 0),
      outputTokens: modelSpans.reduce((total, span) => {
        const value = span.attributes['agent.output_tokens']
        return total + (typeof value === 'number' ? value : 0)
      }, 0),
    }
  })
  const runStatus = computed(() => {
    const status = runSpan.value?.attributes['agent.status']
    return (
      (
        { completed: '完成', failed: '失败', cancelled: '已取消', running: '进行中' } as Record<
          string,
          string
        >
      )[String(status)] || '未知'
    )
  })
  const serviceCount = computed(
    () => new Set(props.detail.spans.map((span) => span.service_name)).size,
  )
  const failedCount = computed(
    () => props.detail.spans.filter((span) => span.status === 'STATUS_CODE_ERROR').length,
  )
  const totalDuration = computed(() => {
    const starts = props.detail.spans
      .map((span) => Date.parse(span.started_at))
      .filter(Number.isFinite)
    if (!starts.length) return '—'
    const ends = props.detail.spans
      .map((span) => Date.parse(span.started_at) + span.duration_ms)
      .filter(Number.isFinite)
    return `${(Math.max(...ends) - Math.min(...starts)).toFixed(2)} ms`
  })
  const errorCode = computed(() => {
    const value =
      runSpan.value?.attributes['agent.error_code'] || runSpan.value?.attributes['error.code']
    return typeof value === 'string' ? value : ''
  })
</script>
