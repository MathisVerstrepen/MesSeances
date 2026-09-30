import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { createHmac } from 'node:crypto'
import { setTimeout } from 'node:timers/promises'
import { rmdirSync } from 'node:fs'

const [action, port, publicKey] = process.argv.slice(2)
if (!/^\d+$/.test(port) || !publicKey?.startsWith('ssh-ed25519 ')) {
  throw new Error('Invalid verified Docker host key')
}
const directory = join(homedir(), '.ssh')
const path = join(directory, 'known_hosts')
const endpoint = `[127.0.0.1]:${port}`
const key = publicKey.split(/\s+/).slice(0, 2).join(' ')
mkdirSync(directory, { recursive: true, mode: 0o700 })
const lock = join(directory, '.orca-known-hosts.lock')
let locked = false
for (let attempt = 0; attempt < 100; attempt++) {
  try { mkdirSync(lock, { mode: 0o700 }); locked = true; break }
  catch (error) { if (error.code !== 'EEXIST') throw error }
  await setTimeout(100)
}
if (!locked) throw new Error('Timed out waiting for known_hosts lock')
try {
let text = ''
try { text = readFileSync(path, 'utf8') } catch (error) { if (error.code !== 'ENOENT') throw error }
const matches = (host) => {
  if (host === endpoint) return true
  const parts = host.split('|')
  return parts.length === 4 && parts[1] === '1' && createHmac('sha1', Buffer.from(parts[2], 'base64')).update(endpoint).digest('base64') === parts[3]
}
// Preserve other endpoints, comments and comma-separated aliases.
const lines = text.replace(/\n$/, '').split('\n').flatMap((line) => {
  const fields = line.split(/\s+/)
  if (!fields[0].split(',').some(matches)) return [line]
  if (action === 'remove' && fields.slice(1, 3).join(' ') !== key) return [line]
  const aliases = fields[0].split(',').filter((host) => !matches(host))
  return aliases.length ? [[aliases.join(','), ...fields.slice(1)].join(' ')] : []
})
if (action === 'add') lines.push(`${endpoint} ${key}`)
else if (action !== 'remove') throw new Error('Unknown known-hosts action')
writeFileSync(path, lines.join('\n') + '\n', { mode: 0o600 })
} finally { rmdirSync(lock) }
