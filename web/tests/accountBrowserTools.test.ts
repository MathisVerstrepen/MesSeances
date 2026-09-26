import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import test from 'node:test'

test('reusable CDP helper does not install competing process exit handlers', () => {
  const helper = new URL('../tools/screenshot.mjs', import.meta.url).href
  const result = spawnSync(
    process.execPath,
    [
      '--input-type=module',
      '-e',
      `import assert from 'node:assert/strict';
const signals = ['SIGHUP', 'SIGINT', 'SIGTERM'];
const before = signals.map(signal => process.listenerCount(signal));
const helper = await import(${JSON.stringify(helper)});
assert.equal(typeof helper.CDP, 'function');
assert.deepEqual(signals.map(signal => process.listenerCount(signal)), before);`,
    ],
    { timeout: 10000, encoding: 'utf8' },
  )
  assert.equal(
    result.status,
    0,
    'CDP import must leave process cleanup to its caller',
  )
})
