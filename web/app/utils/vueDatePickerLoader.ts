import type { Component } from 'vue'

let componentPromise: Promise<Component> | undefined

export function loadVueDatePicker(): Promise<Component> {
  componentPromise ??= Promise.all([
    import('@vuepic/vue-datepicker'),
    import('@vuepic/vue-datepicker/dist/main.css')
  ])
    .then(([module]) => module.VueDatePicker)
    .catch((cause: unknown) => {
      componentPromise = undefined
      throw cause
    })

  return componentPromise
}
