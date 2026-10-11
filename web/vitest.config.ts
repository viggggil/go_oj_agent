import { defineConfig } from 'vitest/config'

export default defineConfig({ test: { include: ['src/agent/*.test.ts'] } })
