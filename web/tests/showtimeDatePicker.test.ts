import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, ref, shallowRef } from 'vue'
import type { Ref } from 'vue'
import {
  calendarDateFromDate,
  dateFromCalendarDate,
  formatLongDate,
} from '../app/utils/date.ts'

async function setup<T extends object>(
  name: string,
  bindings: T,
  returned: string,
) {
  const source = await readFile(
    new URL(`../app/components/${name}.vue`, import.meta.url),
    'utf8',
  )
  const parsed = ts.createSourceFile(
    `${name}.ts`,
    source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!,
    ts.ScriptTarget.Latest,
    true,
  )
  const compiled = ts.transpileModule(
    parsed.statements
      .filter((statement) => !ts.isImportDeclaration(statement))
      .map((statement) => statement.getFullText(parsed))
      .join('\n'),
    { compilerOptions: { target: ts.ScriptTarget.ESNext } },
  ).outputText
  // Execute production setup; substitute only Nuxt macros and component IO.
  return new Function(
    ...Object.keys(bindings),
    `${compiled}\nreturn { ${returned} };`,
  )(...Object.values(bindings))
}

test('Escape at the custom trigger closes immediately without selecting or reaching its parent', async () => {
  const props = { selectedDate: '2026-10-16', allowedDates: ['2026-10-16'] }
  const emitted: string[] = []
  // SAFETY: The setup wrapper returns these exact production refs and handlers.
  const picker = (await setup(
    'ShowtimeDatePicker',
    {
      defineProps: () => props,
      withDefaults: (value: typeof props) => value,
      defineEmits: () => (event: string) => emitted.push(event),
      computed,
      ref,
      calendarDateFromDate,
      dateFromCalendarDate,
      formatLongDate,
    },
    'datePicker, isOpen, pickerDate, handleOpen, handleClosed, handleTriggerKeydown',
  )) as {
    datePicker: Ref<{ closeMenu: () => void } | null>
    isOpen: Ref<boolean>
    pickerDate: Ref<Date>
    handleOpen: () => void
    handleClosed: () => void
    handleTriggerKeydown: (event: Event & { key: string }) => void
  }
  let closes = 0
  picker.datePicker.value = {
    closeMenu: () => {
      closes++
      picker.handleClosed()
    },
  }
  const keydown = (key: string) =>
    Object.assign(new Event('keydown', { cancelable: true }), {
      key,
    })
  const idleEscape = keydown('Escape')
  picker.handleTriggerKeydown(idleEscape)
  assert.equal(idleEscape.defaultPrevented, false)
  assert.equal(closes, 0)

  picker.handleOpen()
  const enter = keydown('Enter')
  picker.handleTriggerKeydown(enter)
  assert.equal(enter.defaultPrevented, false)
  assert.equal(picker.isOpen.value, true)

  const selected = picker.pickerDate.value.getTime()
  const escape = keydown('Escape')
  picker.handleTriggerKeydown(escape)
  assert.equal(closes, 1)
  assert.equal(picker.isOpen.value, false)
  assert.equal(escape.defaultPrevented, true)
  assert.equal(escape.cancelBubble, true)
  assert.equal(picker.pickerDate.value.getTime(), selected)
  assert.equal(props.selectedDate, '2026-10-16')
  assert.deepEqual(emitted, ['open', 'closed'])
})

test('deferred picker forwards its supported closeMenu interface to the loaded picker', async () => {
  let exposed: { closeMenu: () => void } | undefined
  const props = { active: false }
  // SAFETY: The setup wrapper returns the actual datePicker template ref.
  const picker = (await setup(
    'DeferredVueDatePicker',
    {
      defineOptions: () => {},
      defineProps: () => props,
      withDefaults: (value: typeof props) => value,
      defineExpose: (value: typeof exposed) => {
        exposed = value
      },
      shallowRef,
      ref,
      onMounted: () => {},
      watch: () => {},
    },
    'datePicker',
  )) as { datePicker: Ref<{ closeMenu: () => void } | null> }
  assert.ok(exposed)
  exposed.closeMenu()
  let closes = 0
  picker.datePicker.value = { closeMenu: () => closes++ }
  exposed.closeMenu()
  assert.equal(closes, 1)
})
