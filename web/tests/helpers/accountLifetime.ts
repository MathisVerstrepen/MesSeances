import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { ref, type Ref } from 'vue'
import { createAccountNavigation } from '../../app/utils/accountNavigation.ts'
import type { useAccountLifetime } from '../../app/composables/useAccountLifetime.ts'

interface LifetimeExports {
  useAccountLifetime?: typeof useAccountLifetime
}

export function lifetimeFixture(revision: Ref<number> = ref(0)) {
  const runtime = createAccountNavigation()
  const unmount: (() => void)[] = []
  const exports: LifetimeExports = {}
  const source = readFileSync(
    new URL('../../app/composables/useAccountLifetime.ts', import.meta.url),
    'utf8',
  )
  runInNewContext(
    ts.transpileModule(source.replaceAll('import.meta.client', 'true'), {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    }).outputText,
    {
      exports,
      useAccountSession: () => ({ revision }),
      useAccountNavigation: () => runtime,
      onBeforeUnmount: (fn: () => void) => unmount.push(fn),
    },
  )
  return {
    useAccountLifetime: exports.useAccountLifetime!,
    useAccountNavigation: () => runtime,
    useRouter: () => ({ currentRoute: ref({ path: '/compte' }) }),
    useRoute: () => ({ path: '/compte' }),
    runtime,
    unmount: () => {
      for (const fn of unmount) fn()
    },
  }
}
