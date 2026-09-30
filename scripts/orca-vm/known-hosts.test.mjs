import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { createHmac } from 'node:crypto'
import { spawnSync } from 'node:child_process'

test('Docker endpoint replacement preserves other hosts and stale destroy cannot remove new key', () => {
  const home = mkdtempSync(join(tmpdir(), 'orca-known-hosts-'))
  try {
    mkdirSync(join(home, '.ssh'))
    const path = join(home, '.ssh', 'known_hosts')
    const endpoint = '[127.0.0.1]:42000'
    const salt = Buffer.from('endpoint salt')
    const hash = `|1|${salt.toString('base64')}|${createHmac('sha1', salt).update(endpoint).digest('base64')}`
    writeFileSync(path, `# existing hosts\nother ssh-ed25519 KEEP\n${hash} ssh-ed25519 OLD\n${endpoint},alias ssh-ed25519 OLD\n`)
    const run = (action, key) => {
      const result = spawnSync(process.execPath, [new URL('./known-hosts.mjs', import.meta.url).pathname, action, '42000', key], { env: { ...process.env, HOME: home }, encoding: 'utf8' })
      assert.equal(result.status, 0, result.stderr)
      return readFileSync(path, 'utf8')
    }
    const replaced = run('add', 'ssh-ed25519 NEW container')
    assert.equal(replaced, `# existing hosts\nother ssh-ed25519 KEEP\nalias ssh-ed25519 OLD\n${endpoint} ssh-ed25519 NEW\n`)
    assert.equal(run('remove', 'ssh-ed25519 OLD container'), replaced)
    assert.equal(run('remove', 'ssh-ed25519 NEW container'), '# existing hosts\nother ssh-ed25519 KEEP\nalias ssh-ed25519 OLD\n')
  } finally { rmSync(home, { recursive: true, force: true }) }
})
